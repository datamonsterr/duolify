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
        let url =
            format!("https://huggingface.co/datasets/{dataset}/resolve/{revision}/golden.jsonl");
        let body = reqwest::Client::new()
            .get(url)
            .bearer_auth(std::env::var("HF_TOKEN")?)
            .send()
            .await?
            .error_for_status()?
            .text()
            .await?;
        let cache = root.join(".cache");
        std::fs::create_dir_all(&cache)?;
        std::fs::write(
            cache.join(format!("{}-{revision}.jsonl", dataset.replace('/', "--"))),
            &body,
        )?;
        body
    };
    let router = engine_providers::router(provider)?;
    let mut results = Vec::new();
    for line in data.lines().filter(|l| !l.trim().is_empty()) {
        let case: Case = serde_json::from_str(line)?;
        let response = router.complete(&case.task, &case.input.to_string()).await?;
        let mut errors = Vec::new();
        for check in &case.checks {
            let valid = match check.kind.as_str() {
                "contains" => response.text.contains(check.value.as_deref().unwrap_or("")),
                "regex" => regex::Regex::new(check.value.as_deref().context("missing regex")?)?
                    .is_match(&response.text),
                "json" => serde_json::from_str::<serde_json::Value>(&response.text).is_ok(),
                "field" => serde_json::from_str::<serde_json::Value>(&response.text)
                    .ok()
                    .is_some_and(|v| v.get(check.value.as_deref().unwrap_or("")).is_some()),
                "slide" => serde_json::from_str::<serde_json::Value>(&response.text)
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
            let judgment = router.complete("chat", &format!("Judge this response against the rubric. Reply PASS or FAIL. Rubric: {}\nResponse: {}",case.rubric,response.text)).await?;
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
