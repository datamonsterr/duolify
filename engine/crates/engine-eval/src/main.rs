use anyhow::{Context, Result, bail};
use serde::{Deserialize, Serialize};
use std::{
    path::PathBuf,
    time::{SystemTime, UNIX_EPOCH},
};
#[derive(Deserialize)]
struct Case {
    id: String,
    task: String,
    input: serde_json::Value,
    rubric: String,
    checks: Vec<Check>,
}
#[derive(Deserialize)]
struct Check {
    kind: String,
    value: Option<String>,
}
#[derive(Serialize)]
struct ResultRow {
    id: String,
    passed: bool,
    errors: Vec<String>,
}
#[derive(Deserialize)]
struct Threshold {
    minimum_score: f64,
}

fn select_cases(mut cases: Vec<Case>, limit: Option<usize>) -> (Vec<Case>, usize) {
    let total = cases.len();
    cases.sort_by(|a, b| a.id.cmp(&b.id));
    if let Some(limit) = limit.filter(|&n| n > 0) {
        cases.truncate(limit);
    }
    (cases, total)
}

fn sample_limit() -> Result<Option<usize>> {
    let mut limit = std::env::var("EVAL_SAMPLE_LIMIT")
        .ok()
        .map(|value| {
            value
                .parse::<usize>()
                .context("EVAL_SAMPLE_LIMIT must be an integer")
        })
        .transpose()?;
    let args: Vec<String> = std::env::args().skip(1).collect();
    let mut i = 0;
    while i < args.len() {
        if args[i] == "--limit" {
            let value = args.get(i + 1).context("--limit requires a value")?;
            limit = Some(value.parse().context("--limit must be an integer")?);
            i += 1;
        }
        i += 1;
    }
    Ok(limit)
}

// Recorded provider output still runs through the production brief processor.
struct RecordedBrief(std::sync::Mutex<std::collections::VecDeque<Option<String>>>);
#[async_trait::async_trait]
impl engine_core::LlmProvider for RecordedBrief {
    fn name(&self) -> &'static str {
        "recorded"
    }
    async fn complete(&self, model: &str, _input: &str) -> Result<engine_core::Completion> {
        let text = self
            .0
            .lock()
            .unwrap()
            .pop_front()
            .flatten()
            .context("recorded provider outage")?;
        Ok(engine_core::Completion {
            text,
            provider: "recorded".into(),
            model: model.into(),
            input_tokens: 120,
            output_tokens: 8,
        })
    }
    async fn stream(&self, _model: &str, _input: &str) -> Result<engine_core::CompletionStream> {
        bail!("brief is not streaming")
    }
}
async fn evaluate_brief(input: &serde_json::Value) -> Result<String> {
    use engine_core::courses::{Brief, Limits, process};
    let recorded: Vec<Option<String>> = serde_json::from_value(input["recorded"].clone())?;
    let provider = std::sync::Arc::new(RecordedBrief(std::sync::Mutex::new(recorded.into())));
    let router = engine_core::ProviderRouter::new(provider.clone(), provider)?;
    let brief: Brief = serde_json::from_value(input["brief"].clone())?;
    let limits = Limits {
        input_tokens: 2000,
        output_tokens: 1000,
        attempt_timeout_ms: 3000,
        max_attempts: 3,
        max_cost_nanos: 2100000,
        input_nanos_per_token: 100,
        output_nanos_per_token: 500,
    };
    let outcome = process(
        &router,
        brief,
        input["step"].as_str().context("step")?,
        input["answer"].as_str().unwrap_or_default(),
        input["skipped"].as_bool().unwrap_or(false),
        input["is_follow_up"].as_bool().unwrap_or(false),
        &limits,
    )
    .await?;
    let mut value = if let Some(result) = outcome.result {
        serde_json::to_value(result)?
    } else {
        serde_json::json!({"form_fallback":true})
    };
    value["attempt_count"] = serde_json::json!(outcome.attempts.len());
    if let Some(confirmed) = input.get("confirmed_brief") {
        let confirmed: Brief = serde_json::from_value(confirmed.clone())?;
        value["confirmation_valid"] = serde_json::json!(
            serde_json::from_value::<Brief>(value["brief"].clone())
                .ok()
                .is_some_and(|b| b == confirmed)
        );
    }
    Ok(value.to_string())
}

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
    let limit = sample_limit()?;
    let root = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../../evals");
    let data = if offline || std::env::var("HF_TOKEN").unwrap_or_default().is_empty() {
        std::fs::read_to_string(root.join("fixtures/golden.sample.jsonl"))?
    } else {
        let dataset = std::env::var("HF_GOLDEN_DATASET")?;
        let revision = std::env::var("HF_GOLDEN_REVISION")?;
        if !dataset
            .chars()
            .all(|c| c.is_ascii_alphanumeric() || "-_/".contains(c))
            || !revision
                .chars()
                .all(|c| c.is_ascii_alphanumeric() || "-_.".contains(c))
        {
            bail!("invalid dataset or revision");
        }
        let cache = root.join(".cache");
        std::fs::create_dir_all(&cache)?;
        let cache_file = cache.join(format!("{}-{revision}.jsonl", dataset.replace('/', "--")));
        let immutable = revision.len() == 40 && revision.bytes().all(|b| b.is_ascii_hexdigit());
        if immutable && cache_file.is_file() {
            std::fs::read_to_string(cache_file)?
        } else {
            let url = format!(
                "https://huggingface.co/datasets/{dataset}/resolve/{revision}/golden.jsonl"
            );
            let body = reqwest::Client::new()
                .get(url)
                .bearer_auth(std::env::var("HF_TOKEN")?)
                .send()
                .await?
                .error_for_status()?
                .text()
                .await?;
            std::fs::write(cache_file, &body)?;
            body
        }
    };
    let router = engine_providers::router(provider)?;
    let mut cases = Vec::new();
    for line in data.lines().filter(|l| !l.trim().is_empty()) {
        cases.push(serde_json::from_str(line)?);
    }
    let (cases, total) = select_cases(cases, limit);
    println!("selected {} of {total} cases", cases.len());
    let mut results = Vec::new();
    for case in cases {
        let response_text = if case.task == "course_brief_chat" && provider == "mock" {
            evaluate_brief(&case.input).await?
        } else {
            router
                .complete(&case.task, &case.input.to_string())
                .await?
                .text
        };
        let mut errors = Vec::new();
        for check in &case.checks {
            let valid = match check.kind.as_str() {
                "path_equals" => {
                    let spec: serde_json::Value =
                        serde_json::from_str(check.value.as_deref().context("path check")?)?;
                    let value: serde_json::Value = serde_json::from_str(&response_text)?;
                    let actual = spec["path"]
                        .as_str()
                        .unwrap_or_default()
                        .split('.')
                        .fold(&value, |v, key| &v[key]);
                    actual == &spec["expected"]
                }
                "contains" => response_text.contains(check.value.as_deref().unwrap_or("")),
                "regex" => regex::Regex::new(check.value.as_deref().context("missing regex")?)?
                    .is_match(&response_text),
                "json" => serde_json::from_str::<serde_json::Value>(&response_text).is_ok(),
                "field" => serde_json::from_str::<serde_json::Value>(&response_text)
                    .ok()
                    .is_some_and(|v| v.get(check.value.as_deref().unwrap_or("")).is_some()),
                "slide" => serde_json::from_str::<serde_json::Value>(&response_text)
                    .ok()
                    .and_then(|v| v["html"].as_str().map(str::to_owned))
                    .is_some_and(|h| engine_core::slide::validate(&h, &[]).is_ok()),
                _ => false,
            };
            if !valid {
                errors.push(format!("{} failed", check.kind));
            }
        }
        if judge {
            let judgment = router.complete("chat", &format!("Judge this response against the rubric. Reply PASS or FAIL. Rubric: {}\nResponse: {}",case.rubric,response_text)).await?;
            if judgment.text.trim() != "PASS" {
                errors.push("judge failed".into());
            }
        }
        results.push(ResultRow {
            id: case.id,
            passed: errors.is_empty(),
            errors,
        });
    }
    if results.is_empty() {
        bail!("golden set is empty");
    }
    let score = results.iter().filter(|r| r.passed).count() as f64 / results.len() as f64;
    let threshold: Threshold =
        toml::from_str(&std::fs::read_to_string(root.join("thresholds.toml"))?)?;
    let reports = root.join("reports");
    std::fs::create_dir_all(&reports)?;
    let stamp = SystemTime::now().duration_since(UNIX_EPOCH)?.as_millis();
    std::fs::write(
        reports.join(format!("{stamp}.json")),
        serde_json::to_vec_pretty(&results)?,
    )?;
    std::fs::write(
        reports.join(format!("{stamp}.md")),
        format!(
            "# Eval report\n\nScore: {score:.2} (threshold {:.2})\n",
            threshold.minimum_score
        ),
    )?;
    println!("score {score:.2}; threshold {:.2}", threshold.minimum_score);
    if score < threshold.minimum_score {
        bail!("score below threshold");
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::{Case, select_cases};

    fn cases() -> Vec<Case> {
        ["c", "a", "b"]
            .into_iter()
            .map(|id| Case {
                id: id.into(),
                task: String::new(),
                input: serde_json::Value::Null,
                rubric: String::new(),
                checks: Vec::new(),
            })
            .collect()
    }

    #[test]
    fn selection_is_sorted_and_respects_optional_limit() {
        for limit in [None, Some(0)] {
            let (selected, total) = select_cases(cases(), limit);
            assert_eq!(total, 3);
            assert_eq!(
                selected.iter().map(|c| c.id.as_str()).collect::<Vec<_>>(),
                ["a", "b", "c"]
            );
        }
        let (selected, _) = select_cases(cases(), Some(2));
        assert_eq!(
            selected.iter().map(|c| c.id.as_str()).collect::<Vec<_>>(),
            ["a", "b"]
        );
        let (selected, _) = select_cases(cases(), Some(9));
        assert_eq!(
            selected.iter().map(|c| c.id.as_str()).collect::<Vec<_>>(),
            ["a", "b", "c"]
        );
    }
}
