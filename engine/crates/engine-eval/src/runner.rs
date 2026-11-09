use crate::{
    judge::parse_judge_verdict,
    scoring::{Case, CaseResult, EvalReport, Thresholds, aggregate_task_reports},
    tasks::{canonical_task_name, model_route_for_task},
};
use anyhow::{Context, Result, bail};
use std::{
    collections::{HashMap, VecDeque},
    path::Path,
    sync::{Arc, Mutex},
    time::{SystemTime, UNIX_EPOCH},
};

pub fn select_cases(mut cases: Vec<Case>, limit: Option<usize>) -> (Vec<Case>, usize) {
    let total = cases.len();
    cases.sort_by(|a, b| a.id.cmp(&b.id));
    if let Some(limit) = limit.filter(|&n| n > 0) {
        cases.truncate(limit);
    }
    (cases, total)
}

pub fn sample_limit_from_args(args: &[String], env_var: Option<&str>) -> Result<Option<usize>> {
    let mut limit = env_var
        .map(|v| {
            v.parse::<usize>()
                .context("EVAL_SAMPLE_LIMIT must be an integer")
        })
        .transpose()?;

    let mut i = 0;
    while i < args.len() {
        if args[i] == "--limit" {
            let val = args.get(i + 1).context("--limit requires a value")?;
            limit = Some(val.parse().context("--limit must be an integer")?);
            i += 1;
        }
        i += 1;
    }
    Ok(limit)
}

pub fn compute_sha256(data: &[u8]) -> String {
    let digest = ring::digest::digest(&ring::digest::SHA256, data);
    digest
        .as_ref()
        .iter()
        .map(|b| format!("{:02x}", b))
        .collect()
}

pub fn is_pinned_commit(revision: &str) -> bool {
    revision.len() == 40 && revision.bytes().all(|b| b.is_ascii_hexdigit())
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FetchedDataset {
    pub content: String,
    pub source: String, // "cache", "hf", "mirror", "fixture"
    pub revision: String,
    pub checksum: String,
}

impl std::ops::Deref for FetchedDataset {
    type Target = str;
    fn deref(&self) -> &Self::Target {
        &self.content
    }
}

impl AsRef<str> for FetchedDataset {
    fn as_ref(&self) -> &str {
        &self.content
    }
}

impl From<FetchedDataset> for String {
    fn from(f: FetchedDataset) -> Self {
        f.content
    }
}

pub async fn fetch_dataset(
    base_url: &str,
    dataset: &str,
    revision: &str,
    token: &str,
    cache_dir: &Path,
) -> Result<FetchedDataset> {
    let mirror = std::env::var("HF_GOLDEN_MIRROR_URL")
        .or_else(|_| std::env::var("GOLDEN_MIRROR_URL"))
        .ok();
    fetch_dataset_with_mirror(
        base_url,
        mirror.as_deref(),
        dataset,
        revision,
        token,
        cache_dir,
    )
    .await
}

pub async fn fetch_dataset_with_mirror(
    base_url: &str,
    mirror_url: Option<&str>,
    dataset: &str,
    revision: &str,
    token: &str,
    cache_dir: &Path,
) -> Result<FetchedDataset> {
    if !dataset
        .chars()
        .all(|c| c.is_ascii_alphanumeric() || "-_/".contains(c))
        || !revision
            .chars()
            .all(|c| c.is_ascii_alphanumeric() || "-_.".contains(c))
    {
        bail!("invalid dataset or revision");
    }

    if !is_pinned_commit(revision) {
        bail!("dataset revision must be a pinned 40-character commit hash: '{revision}'");
    }

    std::fs::create_dir_all(cache_dir)?;
    let cache_file = cache_dir.join(format!("{}-{revision}.jsonl", dataset.replace('/', "--")));
    let checksum_file = cache_dir.join(format!("{}-{revision}.sha256", dataset.replace('/', "--")));

    // 1. Check local cache
    if cache_file.is_file() {
        let content = std::fs::read_to_string(&cache_file)
            .with_context(|| format!("failed to read cache file at {}", cache_file.display()))?;
        let computed = compute_sha256(content.as_bytes());

        if checksum_file.is_file() {
            let expected = std::fs::read_to_string(&checksum_file)?.trim().to_string();
            if computed != expected {
                bail!(
                    "cache corruption detected: checksum mismatch for {} (got {computed}, expected {expected})",
                    cache_file.display()
                );
            }
        } else {
            std::fs::write(&checksum_file, &computed)?;
        }

        return Ok(FetchedDataset {
            content,
            source: "cache".to_string(),
            revision: revision.to_string(),
            checksum: computed,
        });
    }

    // 2. Try primary Hugging Face endpoint
    let hf_url = format!(
        "{}/datasets/{dataset}/resolve/{revision}/golden.jsonl",
        base_url.trim_end_matches('/')
    );
    let mut req = reqwest::Client::new().get(&hf_url);
    if !token.is_empty() {
        req = req.bearer_auth(token);
    }

    let hf_result: Result<String> = async {
        let res = req.send().await?;
        let status = res.status();
        if !status.is_success() {
            bail!("HTTP status {status} for url ({hf_url})");
        }
        let body = res.text().await?;
        Ok(body)
    }
    .await;

    match hf_result {
        Ok(body) => {
            let checksum = compute_sha256(body.as_bytes());
            std::fs::write(&cache_file, &body)?;
            std::fs::write(&checksum_file, &checksum)?;
            Ok(FetchedDataset {
                content: body,
                source: "hf".to_string(),
                revision: revision.to_string(),
                checksum,
            })
        }
        Err(hf_err) => {
            // 3. Fallback to mirror if available
            let mirror_base = mirror_url
                .map(|s| s.to_string())
                .or_else(|| std::env::var("HF_GOLDEN_MIRROR_URL").ok())
                .or_else(|| std::env::var("GOLDEN_MIRROR_URL").ok());

            let mirror_base = match mirror_base {
                Some(m) if !m.trim().is_empty() => m,
                _ => {
                    bail!("Hugging Face download failed ({hf_err}) and no mirror configured");
                }
            };

            let candidate_urls = if mirror_base.contains("{revision}") {
                vec![
                    mirror_base
                        .replace("{dataset}", dataset)
                        .replace("{revision}", revision),
                ]
            } else {
                let m = mirror_base.trim_end_matches('/');
                vec![
                    format!("{m}/{dataset}/{revision}/golden.jsonl"),
                    format!("{m}/datasets/{dataset}/resolve/{revision}/golden.jsonl"),
                    format!("{m}/{revision}/golden.jsonl"),
                ]
            };

            let mut last_mirror_err = None;
            for candidate_url in candidate_urls {
                let client = reqwest::Client::new();
                let resp = match client.get(&candidate_url).send().await {
                    Ok(r) if r.status().is_success() => r,
                    Ok(r) => {
                        last_mirror_err =
                            Some(format!("HTTP status {} for {}", r.status(), candidate_url));
                        continue;
                    }
                    Err(e) => {
                        last_mirror_err = Some(e.to_string());
                        continue;
                    }
                };

                let advertised_checksum = resp
                    .headers()
                    .get("x-checksum-sha256")
                    .or_else(|| resp.headers().get("x-amz-meta-checksum-sha256"))
                    .or_else(|| resp.headers().get("x-amz-checksum-sha256"))
                    .and_then(|v| v.to_str().ok())
                    .map(|s| s.trim().to_string());

                let body = match resp.text().await {
                    Ok(b) => b,
                    Err(e) => {
                        last_mirror_err = Some(e.to_string());
                        continue;
                    }
                };

                let checksum = compute_sha256(body.as_bytes());

                // If advertised checksum was provided in header, verify it
                if advertised_checksum
                    .as_deref()
                    .is_some_and(|exp| exp != checksum)
                {
                    bail!(
                        "mirror integrity failure: checksum mismatch for mirror dataset at {candidate_url} (computed {checksum}, advertised {})",
                        advertised_checksum.as_deref().unwrap_or_default()
                    );
                }

                // If mirror provides a separate .sha256 file, check it
                let checksum_url = format!("{candidate_url}.sha256");
                if let Ok(chk_resp) = client.get(&checksum_url).send().await
                    && chk_resp.status().is_success()
                    && let Ok(chk_text) = chk_resp.text().await
                {
                    let expected = chk_text.trim();
                    if !expected.is_empty() && expected != checksum {
                        bail!(
                            "mirror integrity failure: checksum mismatch for mirror dataset at {candidate_url} (computed {checksum}, expected in .sha256 {expected})"
                        );
                    }
                }

                // Verify dataset content is valid JSONL rows
                let mut lines_count = 0;
                let mut has_json_error = false;
                for line in body.lines().filter(|l| !l.trim().is_empty()) {
                    lines_count += 1;
                    if serde_json::from_str::<serde_json::Value>(line).is_err() {
                        has_json_error = true;
                        break;
                    }
                }
                if has_json_error || lines_count == 0 {
                    bail!(
                        "mirror dataset at {candidate_url} is corrupt: invalid JSON lines or empty"
                    );
                }

                std::fs::write(&cache_file, &body)?;
                std::fs::write(&checksum_file, &checksum)?;

                return Ok(FetchedDataset {
                    content: body,
                    source: "mirror".to_string(),
                    revision: revision.to_string(),
                    checksum,
                });
            }

            bail!(
                "failed to fetch golden set revision {revision}: Hugging Face failed ({hf_err}) and mirror recovery failed ({})",
                last_mirror_err.unwrap_or_else(|| "no mirror candidate matched".into())
            );
        }
    }
}

// Recorded provider output runs through production processors
struct RecordedBrief(Mutex<VecDeque<Option<String>>>);
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
            cache_write_tokens: 0,
            cache_read_tokens: 0,
        })
    }
    async fn stream(&self, _model: &str, _input: &str) -> Result<engine_core::CompletionStream> {
        bail!("brief is not streaming")
    }
}

async fn evaluate_brief(input: &serde_json::Value) -> Result<String> {
    use engine_core::courses::{Brief, Limits, process};
    let recorded: Vec<Option<String>> = serde_json::from_value(input["recorded"].clone())?;
    let provider = Arc::new(RecordedBrief(Mutex::new(recorded.into())));
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

struct RecordedCredits(Mutex<VecDeque<Option<engine_core::Completion>>>);
#[async_trait::async_trait]
impl engine_core::LlmProvider for RecordedCredits {
    fn name(&self) -> &'static str {
        "recorded"
    }
    async fn complete(&self, _model: &str, _input: &str) -> Result<engine_core::Completion> {
        self.0
            .lock()
            .unwrap()
            .pop_front()
            .flatten()
            .context("recorded failure")
    }
    async fn stream(&self, _model: &str, _input: &str) -> Result<engine_core::CompletionStream> {
        bail!("envelope fixture is not streaming")
    }
}

async fn evaluate_credits(input: &serde_json::Value) -> Result<String> {
    let recorded: Vec<Option<engine_core::Completion>> =
        serde_json::from_value(input["recorded"].clone())?;
    let provider = Arc::new(RecordedCredits(Mutex::new(recorded.into())));
    let router = engine_core::ProviderRouter::new(provider.clone(), provider)?;
    let limits: engine_core::credits::Limits = serde_json::from_value(input["limits"].clone())?;
    let outcome = engine_core::credits::run(&router, "preview", "fixture", &limits).await;
    Ok(match outcome {
        Ok(o) => serde_json::json!({
            "delivered": o.completion.is_some(),
            "attempt_count": o.attempts.len(),
            "fallback_used": o.attempts.iter().any(|a| a.fallback_used),
            "usage_unavailable": o.attempts.iter().any(|a| a.usage_unavailable),
            "attempts": o.attempts
        }),
        Err(_) => serde_json::json!({"rejected": true, "delivered": false, "attempt_count": 0}),
    }
    .to_string())
}

async fn evaluate_preview(input: &serde_json::Value) -> Result<String> {
    let recorded: Vec<Option<engine_core::Completion>> =
        serde_json::from_value(input["recorded"].clone())?;
    let provider = Arc::new(RecordedCredits(Mutex::new(recorded.into())));
    let router = engine_core::ProviderRouter::new(provider.clone(), provider)?;
    let limits: engine_core::credits::Limits = serde_json::from_value(input["limits"].clone())?;
    let brief: engine_core::courses::Brief = serde_json::from_value(input["brief"].clone())?;
    let prompt = engine_core::courses::preview_prompt(&brief, input["profile"].clone());
    let outcome = engine_core::credits::run_validated(
        &router,
        "preview",
        &prompt,
        &limits,
        engine_core::courses::validate_preview,
    )
    .await?;
    Ok(serde_json::json!({
        "delivered": outcome.completion.is_some(),
        "attempt_count": outcome.attempts.len(),
        "fallback_used": outcome.attempts.iter().any(|a| a.fallback_used),
        "preview": outcome.completion.and_then(|c| serde_json::from_str::<serde_json::Value>(&c.text).ok())
    })
    .to_string())
}

async fn evaluate_course_outline(input: &serde_json::Value) -> Result<String> {
    use engine_core::courses::outline;
    let revision = input["revision"].as_i64().unwrap_or(1);
    let expected_revision = input["expected_revision"].as_i64().unwrap_or(revision);
    if revision != expected_revision || revision < 1 {
        return Ok(serde_json::json!({
            "delivered": false,
            "stale_revision": true,
            "rejected": true,
            "attempt_count": 0,
        })
        .to_string());
    }
    let base: Vec<outline::Unit> = match serde_json::from_value(input["outline"].clone()) {
        Ok(units) => units,
        Err(_) => {
            return Ok(serde_json::json!({
                "delivered": false,
                "invalid_structure": true,
                "rejected": true,
                "attempt_count": 0,
            })
            .to_string());
        }
    };
    let brief = input
        .get("brief")
        .cloned()
        .unwrap_or_else(|| serde_json::json!({}));
    let profile = input
        .get("profile")
        .cloned()
        .unwrap_or_else(|| serde_json::json!({}));
    let prompt = match outline::creation_prompt(&base, brief, profile) {
        Ok(p) => p,
        Err(_) => {
            return Ok(serde_json::json!({
                "delivered": false,
                "invalid_structure": true,
                "rejected": true,
                "attempt_count": 0,
            })
            .to_string());
        }
    };
    let recorded: Vec<Option<engine_core::Completion>> =
        serde_json::from_value(input.get("recorded").cloned().unwrap_or_default())
            .unwrap_or_default();
    let provider = Arc::new(RecordedCredits(Mutex::new(recorded.into())));
    let router = engine_core::ProviderRouter::new(provider.clone(), provider)?;
    let limits: engine_core::credits::Limits =
        serde_json::from_value(input.get("limits").cloned().unwrap_or_else(|| {
            serde_json::json!({
                "input_tokens": 12000,
                "output_tokens": 2000,
                "timeout_ms": 10000,
                "max_attempts": 2,
                "max_cost_nanos": 100000000,
                "input_nanos": 100,
                "output_nanos": 500,
                "cache_write_nanos": 125,
                "cache_read_nanos": 10
            })
        }))?;
    let outcome =
        engine_core::credits::run_validated(&router, "outline", &prompt, &limits, |text| {
            outline::validate_created(&base, text)
        })
        .await;
    match outcome {
        Ok(o) => {
            let units: Option<Vec<outline::Unit>> = o
                .completion
                .as_ref()
                .and_then(|c| serde_json::from_str::<serde_json::Value>(&c.text).ok())
                .and_then(|v| serde_json::from_value(v["units"].clone()).ok());
            let adhered = units.as_ref().is_some_and(|u| u == &base);
            Ok(serde_json::json!({
                "delivered": o.completion.is_some(),
                "attempt_count": o.attempts.len(),
                "fallback_used": o.attempts.iter().any(|a| a.fallback_used),
                "adhered": adhered,
                "units": units,
            })
            .to_string())
        }
        Err(_) => Ok(serde_json::json!({
            "delivered": false,
            "rejected": true,
            "attempt_count": 0,
        })
        .to_string()),
    }
}

async fn evaluate_outline_change(input: &serde_json::Value) -> Result<String> {
    use engine_core::courses::outline;
    let base: Vec<outline::Unit> = serde_json::from_value(input["outline"].clone())?;
    let prompt = outline::prompt(&base, input["request"].as_str().context("request")?)?;
    let recorded: Vec<Option<engine_core::Completion>> =
        serde_json::from_value(input["recorded"].clone())?;
    let provider = Arc::new(RecordedCredits(Mutex::new(recorded.into())));
    let router = engine_core::ProviderRouter::new(provider.clone(), provider)?;
    let limits: engine_core::credits::Limits = serde_json::from_value(input["limits"].clone())?;
    let outcome = engine_core::credits::run_validated(
        &router,
        "outline_chat_change",
        &prompt,
        &limits,
        |text| outline::parse(&base, text).is_ok(),
    )
    .await?;
    let result = outcome
        .completion
        .as_ref()
        .and_then(|c| outline::parse(&base, &c.text).ok())
        .and_then(|ops| outline::apply(&base, &ops).ok());
    Ok(serde_json::json!({
        "delivered": result.is_some(),
        "result": result,
        "attempt_count": outcome.attempts.len(),
        "fallback_used": outcome.attempts.iter().any(|a| a.fallback_used)
    })
    .to_string())
}

pub struct Runner {
    pub provider_name: String,
    pub judge: bool,
    pub dataset_name: String,
    pub dataset_revision: String,
    pub dataset_source: String,
    pub prompt_revision: String,
    pub judge_fixtures: HashMap<String, String>,
}

impl Runner {
    pub fn new(provider_name: &str, judge: bool) -> Self {
        Self {
            provider_name: provider_name.to_string(),
            judge,
            dataset_name: "datamonsterr/duolify-golden".to_string(),
            dataset_revision: "pinned".to_string(),
            dataset_source: "fixture".to_string(),
            prompt_revision: "head".to_string(),
            judge_fixtures: HashMap::new(),
        }
    }

    pub fn mock_with_judge_judgments(judgments: Vec<(String, String)>) -> Self {
        let mut r = Self::new("mock", true);
        for (id, verdict) in judgments {
            r.judge_fixtures.insert(id, verdict);
        }
        r
    }

    pub async fn run_cases(&self, cases: Vec<Case>, thresholds: &Thresholds) -> Result<EvalReport> {
        let router = engine_providers::router(&self.provider_name)?;
        let mut results = Vec::new();
        let mut models = HashMap::new();

        for case in &cases {
            let canonical = canonical_task_name(&case.task);
            let route = model_route_for_task(&case.task);
            models
                .entry(canonical.clone())
                .or_insert_with(|| route.clone());

            let response_text =
                if case.input.get("recorded").is_some() && self.provider_name == "mock" {
                    if canonical == "course_outline" {
                        evaluate_course_outline(&case.input).await?
                    } else if canonical == "course_preview" {
                        evaluate_preview(&case.input).await?
                    } else if case.task == "outline_chat_change" {
                        evaluate_outline_change(&case.input).await?
                    } else if case.task == "credit_envelope" {
                        evaluate_credits(&case.input).await?
                    } else if case.task == "course_brief_chat" {
                        evaluate_brief(&case.input).await?
                    } else {
                        router.complete(&route, &case.input.to_string()).await?.text
                    }
                } else if canonical == "course_outline"
                    && self.provider_name == "mock"
                    && case.input.get("outline").is_some()
                {
                    evaluate_course_outline(&case.input).await?
                } else {
                    router.complete(&route, &case.input.to_string()).await?.text
                };

            let mut errors = Vec::new();
            let mut critical_failed = false;

            for check in &case.checks {
                let valid = match check.kind.as_str() {
                    "path_equals" => {
                        let spec: serde_json::Value = serde_json::from_str(
                            check.value.as_deref().context("missing path_equals spec")?,
                        )?;
                        let value: serde_json::Value = serde_json::from_str(&response_text)?;
                        let actual = spec["path"].as_str().unwrap_or_default().split('.').fold(
                            &value,
                            |v, key| {
                                if let Ok(idx) = key.parse::<usize>() {
                                    v.get(idx).unwrap_or(&serde_json::Value::Null)
                                } else {
                                    &v[key]
                                }
                            },
                        );
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
                    let err = format!("{} check failed", check.kind);
                    if check.critical {
                        critical_failed = true;
                        errors.push(format!("CRITICAL: {err}"));
                    } else {
                        errors.push(err);
                    }
                }
            }

            let deterministic_passed = errors.is_empty();

            let judge_passed = if self.judge {
                let verdict_str = if let Some(preset) = self.judge_fixtures.get(&case.id) {
                    preset.clone()
                } else if self.provider_name == "mock" {
                    // In mock mode without an explicit calibration preset, default to PASS
                    "PASS".to_string()
                } else {
                    let judgment = router
                        .complete(
                            "chat",
                            &format!(
                                "Judge this response against the rubric. Reply PASS or FAIL. Rubric: {}\nResponse: {}",
                                case.rubric, response_text
                            ),
                        )
                        .await?;
                    judgment.text
                };

                let pass = parse_judge_verdict(&verdict_str).unwrap_or(false);
                if !pass {
                    errors.push("judge check failed".into());
                }
                Some(pass)
            } else {
                None
            };

            let passed = deterministic_passed && judge_passed.unwrap_or(true);

            results.push(CaseResult {
                id: case.id.clone(),
                task: canonical,
                passed,
                critical_failed,
                deterministic_passed,
                judge_passed,
                errors,
            });
        }

        let (task_reports, overall_passed, critical_failure) =
            aggregate_task_reports(&results, thresholds, &models);

        let stamp = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_millis();

        Ok(EvalReport {
            timestamp_millis: stamp,
            dataset: self.dataset_name.clone(),
            dataset_revision: self.dataset_revision.clone(),
            dataset_source: self.dataset_source.clone(),
            prompt_revision: self.prompt_revision.clone(),
            passed: overall_passed,
            critical_failure,
            task_reports,
            results,
        })
    }
}
