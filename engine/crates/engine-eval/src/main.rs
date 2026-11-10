use anyhow::{Context, Result, bail};
use engine_eval::{
    Case, EvalReport, Runner, TaskRegistry, Thresholds, budget_from_args, emit_eval_run_completed,
    fetch_dataset_with_mirror, generate_json_report, generate_markdown_report, is_pinned_commit,
    mode_from_args, sample_limit_from_args, select_cases,
};
use std::path::PathBuf;

#[tokio::main]
async fn main() -> Result<()> {
    let args: Vec<String> = std::env::args().collect();
    if args.len() >= 2 && args[1] == "gate" {
        let report_path = args
            .get(2)
            .context("usage: duolify-eval gate <report.json>")?;
        let path = PathBuf::from(report_path);
        let resolved = if path.exists() {
            path
        } else if let Ok(cwd) = std::env::current_dir() {
            let candidate = cwd.join(&path);
            if candidate.exists() {
                candidate
            } else if let Some(parent) = cwd.parent() {
                let parent_candidate = parent.join(&path);
                if parent_candidate.exists() {
                    parent_candidate
                } else {
                    path
                }
            } else {
                path
            }
        } else {
            path
        };

        let content = std::fs::read_to_string(&resolved)
            .with_context(|| format!("failed to read report file at {}", resolved.display()))?;
        let report: EvalReport = serde_json::from_str(&content)
            .with_context(|| format!("failed to parse eval report at {}", resolved.display()))?;

        let status_lower = report.status.to_ascii_lowercase();
        if status_lower.contains("missing")
            && (status_lower.contains("secret") || status_lower.contains("config"))
        {
            let reason = &report.status;
            println!("live gate disabled: {reason}, feature activation stays disabled");
            return Ok(());
        }

        engine_eval::check_promotion_gate(&report)?;
        println!("promotion gate: PASSED ({})", resolved.display());
        return Ok(());
    }

    let (mode, allow_missing_secrets) = mode_from_args(&args);
    let mut offline = args.iter().any(|a| a == "--offline") || mode.as_deref() == Some("draft");
    let explicit_provider = args
        .windows(2)
        .find(|a| a[0] == "--provider")
        .map(|a| a[1].as_str());
    if mode.as_deref() == Some("draft") {
        if let Some(p) = explicit_provider
            && p != "mock"
        {
            bail!(
                "--mode draft cannot be combined with --provider {p}; draft runs must use mock provider"
            );
        }
        offline = true;
    }
    let mut provider = explicit_provider.unwrap_or(if offline { "mock" } else { "live" });
    if mode.as_deref() == Some("draft") {
        offline = true;
        provider = "mock";
    }
    if provider != "mock" && provider != "live" {
        bail!("unknown provider: '{provider}'");
    }
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
    let budget = budget_from_args(&args, std::env::var("EVAL_BUDGET_USD").ok().as_deref())?;
    let root = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../../evals");
    if custom_reports.is_none() {
        let default_reports = root.join("reports");
        let _ = std::fs::remove_file(default_reports.join("latest.json"));
        let _ = std::fs::remove_file(default_reports.join("latest.md"));
    }

    // Check ready pipeline mode with missing credentials or missing revision config
    let is_ready = mode.as_deref() == Some("ready") || allow_missing_secrets;
    if !offline && is_ready {
        let hf_rev_opt = std::env::var("HF_GOLDEN_REVISION")
            .ok()
            .filter(|r| !r.trim().is_empty());
        let needs_hf = custom_data.is_none();
        if needs_hf && hf_rev_opt.is_none() {
            println!(
                "::notice::Live sample evals skipped: HF_GOLDEN_REVISION missing; feature activation stays disabled without fabricating a live pass."
            );
            println!("eval status: missing_config; live promotion disabled");

            let stamp = std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap_or_default()
                .as_millis();
            let dataset = std::env::var("HF_GOLDEN_DATASET")
                .unwrap_or_else(|_| "datamonsterr/duolify-golden".into());

            let report = EvalReport {
                timestamp_millis: stamp,
                dataset,
                dataset_revision: "none".to_string(),
                dataset_source: "none".to_string(),
                prompt_revision: std::env::var("PROMPT_REVISION")
                    .unwrap_or_else(|_| "git-head".to_string()),
                provider: "none".to_string(),
                budget_usd: budget,
                total_cost_usd: 0.0,
                budget_exceeded: false,
                status: "missing_config".to_string(),
                live_promoted: false,
                passed: false,
                critical_failure: false,
                task_reports: std::collections::BTreeMap::new(),
                results: Vec::new(),
            };

            let reports_dir = custom_reports
                .as_ref()
                .map(PathBuf::from)
                .unwrap_or_else(|| root.join("reports"));
            std::fs::create_dir_all(&reports_dir)?;

            let json_bytes = generate_json_report(&report)?;
            let md_text = generate_markdown_report(&report);

            std::fs::write(reports_dir.join(format!("{stamp}.json")), &json_bytes)?;
            std::fs::write(reports_dir.join(format!("{stamp}.md")), &md_text)?;
            if custom_reports.is_none() {
                let _ = std::fs::write(reports_dir.join("latest.json"), &json_bytes);
                let _ = std::fs::write(reports_dir.join("latest.md"), &md_text);
            }

            // Exit 0 to preserve ci-ok aggregate
            return Ok(());
        }

        let anthropic_key = std::env::var("ANTHROPIC_API_KEY").unwrap_or_default();
        let openrouter_key = std::env::var("OPENROUTER_API_KEY").unwrap_or_default();
        let hf_token = std::env::var("HF_TOKEN").unwrap_or_default();
        let has_secrets = !anthropic_key.is_empty()
            && !openrouter_key.is_empty()
            && (!needs_hf || !hf_token.is_empty());

        if !has_secrets {
            println!(
                "::notice::Live sample evals skipped: credentials unavailable (ANTHROPIC_API_KEY, OPENROUTER_API_KEY, HF_TOKEN); feature activation stays disabled without fabricating a live pass."
            );
            println!("eval status: missing_secrets; live promotion disabled");

            let stamp = std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap_or_default()
                .as_millis();
            let dataset = std::env::var("HF_GOLDEN_DATASET")
                .unwrap_or_else(|_| "datamonsterr/duolify-golden".into());

            let report = EvalReport {
                timestamp_millis: stamp,
                dataset,
                dataset_revision: "none".to_string(),
                dataset_source: "none".to_string(),
                prompt_revision: std::env::var("PROMPT_REVISION")
                    .unwrap_or_else(|_| "git-head".to_string()),
                provider: "none".to_string(),
                budget_usd: budget,
                total_cost_usd: 0.0,
                budget_exceeded: false,
                status: "missing_secrets".to_string(),
                live_promoted: false,
                passed: false,
                critical_failure: false,
                task_reports: std::collections::BTreeMap::new(),
                results: Vec::new(),
            };

            let reports_dir = custom_reports
                .as_ref()
                .map(PathBuf::from)
                .unwrap_or_else(|| root.join("reports"));
            std::fs::create_dir_all(&reports_dir)?;

            let json_bytes = generate_json_report(&report)?;
            let md_text = generate_markdown_report(&report);

            std::fs::write(reports_dir.join(format!("{stamp}.json")), &json_bytes)?;
            std::fs::write(reports_dir.join(format!("{stamp}.md")), &md_text)?;
            if custom_reports.is_none() {
                let _ = std::fs::write(reports_dir.join("latest.json"), &json_bytes);
                let _ = std::fs::write(reports_dir.join("latest.md"), &md_text);
            }

            // Exit 0 to preserve ci-ok aggregate
            return Ok(());
        }
    }

    let (data, dataset_name, dataset_revision, dataset_source) = if let Some(path) = custom_data {
        let content = std::fs::read_to_string(&path)
            .with_context(|| format!("failed to read custom data file at {path}"))?;
        (content, path, "custom".to_string(), "custom".to_string())
    } else if offline {
        let path = root.join("fixtures/golden.sample.jsonl");
        let content = std::fs::read_to_string(&path)
            .with_context(|| format!("failed to read sample fixtures at {:?}", path))?;
        (
            content,
            "evals/fixtures/golden.sample.jsonl".to_string(),
            "sample".to_string(),
            "fixture".to_string(),
        )
    } else {
        let dataset = std::env::var("HF_GOLDEN_DATASET")
            .unwrap_or_else(|_| "datamonsterr/duolify-golden".into());
        let revision = std::env::var("HF_GOLDEN_REVISION")
            .context("HF_GOLDEN_REVISION must be set for live evaluation")?;
        if !is_pinned_commit(&revision) {
            bail!("dataset revision must be a pinned 40-character commit hash: '{revision}'");
        }
        let token = std::env::var("HF_TOKEN").unwrap_or_default();
        let cache_dir = std::env::var("EVAL_CACHE_DIR")
            .map(PathBuf::from)
            .unwrap_or_else(|_| root.join(".cache"));
        let base_url =
            std::env::var("HF_BASE_URL").unwrap_or_else(|_| "https://huggingface.co".to_string());
        let mirror_url = std::env::var("HF_GOLDEN_MIRROR_URL")
            .or_else(|_| std::env::var("GOLDEN_MIRROR_URL"))
            .ok();

        let fetched = fetch_dataset_with_mirror(
            &base_url,
            mirror_url.as_deref(),
            &dataset,
            &revision,
            &token,
            &cache_dir,
        )
        .await?;
        (fetched.content, dataset, revision, fetched.source)
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
    if !is_custom_run {
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

    if !is_custom_run {
        let selected_tasks: Vec<String> = cases.iter().map(|c| c.task.clone()).collect();
        let missing = task_registry.missing_required_tasks(&selected_tasks);
        if !missing.is_empty() {
            bail!(
                "sampled run violates minimum per-task coverage: missing canonical tasks {:?}",
                missing
            );
        }
    }

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
    runner.dataset_source = dataset_source;
    runner.prompt_revision = prompt_rev;
    runner.budget_usd = budget;

    let report = runner.run_cases(cases, &thresholds).await?;

    let reports_dir = custom_reports
        .as_ref()
        .map(PathBuf::from)
        .unwrap_or_else(|| root.join("reports"));
    std::fs::create_dir_all(&reports_dir)?;

    let stamp = report.timestamp_millis;
    let json_bytes = generate_json_report(&report)?;
    let md_text = generate_markdown_report(&report);

    std::fs::write(reports_dir.join(format!("{stamp}.json")), &json_bytes)?;
    std::fs::write(reports_dir.join(format!("{stamp}.md")), &md_text)?;
    if custom_reports.is_none() {
        let _ = std::fs::write(reports_dir.join("latest.json"), &json_bytes);
        let _ = std::fs::write(reports_dir.join("latest.md"), &md_text);
    }

    // Emit analytics for each evaluated task
    let posthog_host =
        std::env::var("POSTHOG_HOST").unwrap_or_else(|_| "https://us.i.posthog.com".to_string());
    let posthog_key = std::env::var("POSTHOG_API_KEY")
        .or_else(|_| std::env::var("POSTHOG_KEY"))
        .unwrap_or_default();

    for (task, tr) in &report.task_reports {
        if let Err(err) = emit_eval_run_completed(
            &posthog_host,
            &posthog_key,
            task,
            tr.score,
            tr.threshold,
            tr.passed,
            &report.dataset_revision,
            &report.dataset_source,
            &report.provider,
            &report.status,
            report.budget_usd,
        )
        .await
        {
            eprintln!("warning: failed to emit eval_run_completed for {task}: {err}");
        }
        println!(
            "task {}: score {:.2} (threshold {:.2}) -> {}",
            task,
            tr.score,
            tr.threshold,
            if tr.passed { "PASS" } else { "FAIL" }
        );
    }

    if report.budget_exceeded {
        eprintln!(
            "eval failed: evaluation budget exceeded (${:.6} >= ${:.6})",
            report.total_cost_usd,
            budget.unwrap_or_default()
        );
        bail!("budget exceeded");
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
        let is_immutable = is_pinned_commit(valid_sha1);
        assert!(is_immutable);

        let branch_rev = "main";
        let is_branch_immutable = is_pinned_commit(branch_rev);
        assert!(!is_branch_immutable);
    }

    #[test]
    fn checksum_verification_computes_sha256_and_detects_mismatch() {
        use engine_eval::compute_sha256;
        let data = b"hello world\n";
        let computed = compute_sha256(data);
        assert_eq!(
            computed,
            "a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a447"
        );
        let mismatched = "0000000000000000000000000000000000000000000000000000000000000000";
        assert_ne!(computed, mismatched);
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
            "0123456789abcdef0123456789abcdef01234567",
            "mirror",
            "live",
            "passed",
            Some(5.0),
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
        assert_eq!(
            props["dataset_revision"],
            "0123456789abcdef0123456789abcdef01234567"
        );
        assert_eq!(props["dataset_source"], "mirror");
        assert_eq!(props["provider"], "live");
        assert_eq!(props["status"], "passed");
        assert_eq!(props["budget_usd"], 5.0);
        assert_eq!(props["source"], "ci");
        assert_eq!(props["platform"], "web");
        assert_eq!(props["app_version"], "development");

        // Assert strictly no private text exists in properties or payload
        let raw = body.to_string();
        assert!(!raw.contains("learner_text"));
        assert!(!raw.contains("prompt"));
        assert!(!raw.contains("completion"));
    }

    #[test]
    fn task_stratified_sample_selection_distributes_across_tasks() {
        let mut cases = Vec::new();
        for task in ["course_preview", "course_outline", "lesson_slides"] {
            for i in 1..=4 {
                cases.push(Case {
                    id: format!("{task}-{i}"),
                    task: task.to_string(),
                    input: serde_json::Value::Null,
                    rubric: String::new(),
                    checks: Vec::new(),
                });
            }
        }
        let (selected, total) = select_cases(cases, Some(6));
        assert_eq!(total, 12);
        assert_eq!(selected.len(), 6);
        let mut by_task = std::collections::HashMap::new();
        for c in &selected {
            *by_task.entry(c.task.clone()).or_insert(0) += 1;
        }
        assert_eq!(by_task.get("course_preview"), Some(&2));
        assert_eq!(by_task.get("course_outline"), Some(&2));
        assert_eq!(by_task.get("lesson_slides"), Some(&2));
    }

    #[test]
    fn promotion_gate_enforces_live_pass_and_rejects_missing_secrets() {
        use engine_eval::is_promotable;
        let report_mock = EvalReport {
            provider: "mock".to_string(),
            passed: true,
            ..Default::default()
        };
        assert!(!is_promotable(&report_mock, "course_preview"));

        let report_missing = EvalReport {
            provider: "live".to_string(),
            status: "missing_secrets".to_string(),
            passed: false,
            ..Default::default()
        };
        assert!(!is_promotable(&report_missing, "course_preview"));
    }

    #[test]
    fn budget_and_mode_argument_parsing() {
        let args = vec!["bin".into(), "--budget".into(), "12.50".into()];
        assert_eq!(budget_from_args(&args, None).unwrap(), Some(12.50));

        let args_env = vec!["bin".into()];
        assert_eq!(
            budget_from_args(&args_env, Some("5.25")).unwrap(),
            Some(5.25)
        );

        let args_ready = vec!["bin".into(), "--ready".into()];
        let (mode, _) = mode_from_args(&args_ready);
        assert_eq!(mode.as_deref(), Some("ready"));

        let args_mode_draft = vec!["bin".into(), "--mode".into(), "draft".into()];
        let (mode_d, _) = mode_from_args(&args_mode_draft);
        assert_eq!(mode_d.as_deref(), Some("draft"));
    }
}
