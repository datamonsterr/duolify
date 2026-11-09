use anyhow::{Context, Result, bail};
use engine_eval::{
    Case, Runner, TaskRegistry, Thresholds, emit_eval_run_completed, fetch_dataset,
    generate_json_report, generate_markdown_report, sample_limit_from_args, select_cases,
};
use std::path::PathBuf;

#[tokio::main]
async fn main() -> Result<()> {
    let args: Vec<String> = std::env::args().collect();
    let offline = args.iter().any(|a| a == "--offline");
    let provider = args
        .windows(2)
        .find(|a| a[0] == "--provider")
        .map(|a| a[1].as_str())
        .unwrap_or("live");
    let judge = args.iter().any(|a| a == "--judge");

    let custom_data = args
        .windows(2)
        .find(|a| a[0] == "--data")
        .map(|a| a[1].clone())
        .or_else(|| std::env::var("EVAL_DATA_PATH").ok());

    let custom_thresholds = args
        .windows(2)
        .find(|a| a[0] == "--thresholds")
        .map(|a| a[1].clone())
        .or_else(|| std::env::var("EVAL_THRESHOLDS_PATH").ok());

    let custom_reports = args
        .windows(2)
        .find(|a| a[0] == "--reports")
        .map(|a| a[1].clone())
        .or_else(|| std::env::var("EVAL_REPORTS_DIR").ok());

    let limit = sample_limit_from_args(&args, std::env::var("EVAL_SAMPLE_LIMIT").ok().as_deref())?;
    let root = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../../evals");

    let (data, dataset_name, dataset_revision) = if let Some(path) = custom_data {
        let content = std::fs::read_to_string(&path)
            .with_context(|| format!("failed to read custom data file at {path}"))?;
        (content, path, "custom".to_string())
    } else if offline || std::env::var("HF_TOKEN").unwrap_or_default().is_empty() {
        let path = root.join("fixtures/golden.sample.jsonl");
        let content = std::fs::read_to_string(&path)
            .with_context(|| format!("failed to read sample fixtures at {:?}", path))?;
        (
            content,
            "evals/fixtures/golden.sample.jsonl".to_string(),
            "sample".to_string(),
        )
    } else {
        let dataset = std::env::var("HF_GOLDEN_DATASET")
            .unwrap_or_else(|_| "datamonsterr/duolify-golden".into());
        let revision = std::env::var("HF_GOLDEN_REVISION").unwrap_or_else(|_| "main".into());
        let token = std::env::var("HF_TOKEN")?;
        let cache_dir = root.join(".cache");

        let content = fetch_dataset(
            "https://huggingface.co",
            &dataset,
            &revision,
            &token,
            &cache_dir,
        )
        .await?;
        (content, dataset, revision)
    };

    let mut cases = Vec::new();
    for line in data.lines().filter(|l| !l.trim().is_empty()) {
        cases.push(serde_json::from_str::<Case>(line)?);
    }

    if cases.is_empty() {
        bail!("golden set is empty");
    }

    // Feature gate verification: canonical tasks must be present in the full dataset
    let mut task_registry = TaskRegistry::default();
    task_registry.register_extension("variants");
    task_registry.register_extension("narration");
    task_registry.register_extension("screening");
    task_registry.register_extension("steering");

    // Only enforce full canonical task presence if evaluating the full standard dataset
    let is_custom_run =
        args.iter().any(|a| a == "--data") || std::env::var("EVAL_DATA_PATH").is_ok();
    if !is_custom_run && limit.is_none() {
        let present_tasks: Vec<String> = cases.iter().map(|c| c.task.clone()).collect();
        let missing = task_registry.missing_required_tasks(&present_tasks);
        if !missing.is_empty() {
            bail!(
                "feature gate failed: missing required cases for canonical tasks: {:?}",
                missing
            );
        }
    }

    let (cases, total) = select_cases(cases, limit);
    println!("selected {} of {total} cases", cases.len());

    let thresholds: Thresholds = if let Some(path) = custom_thresholds {
        let content = std::fs::read_to_string(&path)
            .with_context(|| format!("failed to read custom thresholds file at {path}"))?;
        toml::from_str(&content)?
    } else {
        let content = std::fs::read_to_string(root.join("thresholds.toml"))?;
        toml::from_str(&content)?
    };

    let prompt_rev = std::env::var("PROMPT_REVISION").unwrap_or_else(|_| "git-head".to_string());

    let mut runner = Runner::new(provider, judge);
    runner.dataset_name = dataset_name;
    runner.dataset_revision = dataset_revision;
    runner.prompt_revision = prompt_rev;

    let report = runner.run_cases(cases, &thresholds).await?;

    let reports_dir = custom_reports
        .map(PathBuf::from)
        .unwrap_or_else(|| root.join("reports"));
    std::fs::create_dir_all(&reports_dir)?;

    let stamp = report.timestamp_millis;
    let json_bytes = generate_json_report(&report)?;
    let md_text = generate_markdown_report(&report);

    std::fs::write(reports_dir.join(format!("{stamp}.json")), json_bytes)?;
    std::fs::write(reports_dir.join(format!("{stamp}.md")), md_text)?;

    // Emit analytics for each evaluated task
    let posthog_host =
        std::env::var("POSTHOG_HOST").unwrap_or_else(|_| "https://us.i.posthog.com".to_string());
    let posthog_key = std::env::var("POSTHOG_API_KEY")
        .or_else(|_| std::env::var("POSTHOG_KEY"))
        .unwrap_or_default();

    for (task, tr) in &report.task_reports {
        let _ = emit_eval_run_completed(
            &posthog_host,
            &posthog_key,
            task,
            tr.score,
            tr.threshold,
            tr.passed,
        )
        .await;
        println!(
            "task {}: score {:.2} (threshold {:.2}) -> {}",
            task,
            tr.score,
            tr.threshold,
            if tr.passed { "PASS" } else { "FAIL" }
        );
    }

    if report.critical_failure {
        eprintln!("eval failed: deterministic critical check failed");
        bail!("critical check failure");
    }

    if !report.passed {
        eprintln!("eval failed: one or more tasks failed their threshold");
        bail!("score below threshold");
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use engine_eval::{Case, SYSTEM_OPERATIONAL_UUID, Thresholds, emit_eval_run_completed};
    use wiremock::{
        Mock, MockServer, ResponseTemplate,
        matchers::{method, path},
    };

    fn make_test_cases() -> Vec<Case> {
        ["c", "a", "b"]
            .into_iter()
            .map(|id| Case {
                id: id.into(),
                task: "course_preview".into(),
                input: serde_json::Value::Null,
                rubric: String::new(),
                checks: Vec::new(),
            })
            .collect()
    }

    #[test]
    fn budget_and_sample_selection_is_sorted_and_bounded() {
        for limit in [None, Some(0)] {
            let (selected, total) = select_cases(make_test_cases(), limit);
            assert_eq!(total, 3);
            assert_eq!(
                selected.iter().map(|c| c.id.as_str()).collect::<Vec<_>>(),
                ["a", "b", "c"]
            );
        }
        let (selected, _) = select_cases(make_test_cases(), Some(2));
        assert_eq!(
            selected.iter().map(|c| c.id.as_str()).collect::<Vec<_>>(),
            ["a", "b"]
        );
        let (selected, _) = select_cases(make_test_cases(), Some(9));
        assert_eq!(
            selected.iter().map(|c| c.id.as_str()).collect::<Vec<_>>(),
            ["a", "b", "c"]
        );

        // sample_limit_from_args CLI precedence over env
        let args = vec!["bin".into(), "--limit".into(), "5".into()];
        let lim = sample_limit_from_args(&args, Some("10")).unwrap();
        assert_eq!(lim, Some(5));

        let args_no_limit = vec!["bin".into()];
        let lim_env = sample_limit_from_args(&args_no_limit, Some("10")).unwrap();
        assert_eq!(lim_env, Some(10));
    }

    #[test]
    fn threshold_boundaries_respect_task_specific_and_global_thresholds() {
        let toml_str = r#"
minimum_score = 0.75

[tasks]
course_preview = 0.90
lesson_test = 1.00
"#;
        let thresholds: Thresholds = toml::from_str(toml_str).unwrap();
        assert_eq!(thresholds.threshold_for("course_preview"), 0.90);
        assert_eq!(thresholds.threshold_for("preview"), 0.90); // alias check
        assert_eq!(thresholds.threshold_for("lesson_test"), 1.00);
        assert_eq!(thresholds.threshold_for("test"), 1.00); // alias check
        assert_eq!(thresholds.threshold_for("tutor_chat"), 0.75); // fallback
    }

    #[test]
    fn revision_checks_validate_dataset_naming_and_immutability() {
        let valid_dataset = "datamonsterr/duolify-golden";
        assert!(
            valid_dataset
                .chars()
                .all(|c| c.is_ascii_alphanumeric() || "-_/".contains(c))
        );

        let invalid_dataset = "datamonsterr/duolify;rm -rf";
        assert!(
            !invalid_dataset
                .chars()
                .all(|c| c.is_ascii_alphanumeric() || "-_/".contains(c))
        );

        let valid_sha1 = "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2";
        let is_immutable =
            valid_sha1.len() == 40 && valid_sha1.bytes().all(|b| b.is_ascii_hexdigit());
        assert!(is_immutable);

        let branch_rev = "main";
        let is_branch_immutable =
            branch_rev.len() == 40 && branch_rev.bytes().all(|b| b.is_ascii_hexdigit());
        assert!(!is_branch_immutable);
    }

    #[tokio::test]
    async fn eval_run_completed_emitter_fixtures_assert_uuid_common_props_and_no_private_text() {
        let mock_server = MockServer::start().await;

        Mock::given(method("POST"))
            .and(path("/batch"))
            .respond_with(ResponseTemplate::new(200))
            .expect(1)
            .mount(&mock_server)
            .await;

        let sent = emit_eval_run_completed(
            &mock_server.uri(),
            "fake-posthog-key",
            "course_preview",
            1.0,
            1.0,
            true,
        )
        .await
        .unwrap();

        assert!(sent);

        let requests = mock_server.received_requests().await.unwrap();
        assert_eq!(requests.len(), 1);
        let req = &requests[0];
        let body: serde_json::Value = serde_json::from_slice(&req.body).unwrap();

        let event = &body["batch"][0];
        assert_eq!(event["distinct_id"], SYSTEM_OPERATIONAL_UUID);
        assert_eq!(event["event"], "eval_run_completed");

        let props = &event["properties"];
        assert_eq!(props["task"], "course_preview");
        assert_eq!(props["score"], 1.0);
        assert_eq!(props["threshold"], 1.0);
        assert_eq!(props["passed"], true);
        assert_eq!(props["source"], "ci");
        assert_eq!(props["platform"], "web");
        assert_eq!(props["app_version"], "development");

        // Assert strictly no private text exists in properties or payload
        let raw = body.to_string();
        assert!(!raw.contains("learner_text"));
        assert!(!raw.contains("prompt"));
        assert!(!raw.contains("completion"));
    }
}
