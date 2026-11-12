use crate::{
    judge::parse_judge_verdict,
    scoring::{
        Case, CaseResult, EvalReport, Thresholds, aggregate_task_reports, check_promotion_gate,
    },
    tasks::{canonical_task_name, model_route_for_task},
};
use anyhow::{Context, Result, bail};
use std::{
    collections::{HashMap, VecDeque},
    path::Path,
    sync::{Arc, Mutex},
    time::{SystemTime, UNIX_EPOCH},
};

pub fn select_cases(cases: Vec<Case>, limit: Option<usize>) -> (Vec<Case>, usize) {
    let total = cases.len();
    let limit = match limit.filter(|&n| n > 0) {
        Some(l) => l,
        None => {
            let mut sorted = cases;
            sorted.sort_by(|a, b| a.id.cmp(&b.id));
            return (sorted, total);
        }
    };

    if limit >= total {
        let mut sorted = cases;
        sorted.sort_by(|a, b| a.id.cmp(&b.id));
        return (sorted, total);
    }

    // Group cases by canonical task for task-stratified bounded sampling
    let mut by_task: std::collections::BTreeMap<String, Vec<Case>> =
        std::collections::BTreeMap::new();
    for case in cases {
        let canonical = canonical_task_name(&case.task);
        by_task.entry(canonical).or_default().push(case);
    }

    // Sort cases within each task deterministically by id
    for list in by_task.values_mut() {
        list.sort_by(|a, b| a.id.cmp(&b.id));
        list.reverse(); // Pop smallest id first from the end
    }

    // Round-robin selection across sorted tasks
    let mut selected = Vec::new();
    let task_keys: Vec<String> = by_task.keys().cloned().collect();

    while selected.len() < limit {
        let mut picked_any = false;
        for key in &task_keys {
            if selected.len() >= limit {
                break;
            }
            if let Some(cases_for_task) = by_task.get_mut(key)
                && let Some(c) = cases_for_task.pop()
            {
                selected.push(c);
                picked_any = true;
            }
        }
        if !picked_any {
            break;
        }
    }

    // Deterministic overall sorting by id for consistency
    selected.sort_by(|a, b| a.id.cmp(&b.id));
    (selected, total)
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

pub fn budget_from_args(args: &[String], env_var: Option<&str>) -> Result<Option<f64>> {
    let mut budget = env_var
        .map(|v| v.parse::<f64>().context("EVAL_BUDGET_USD must be a float"))
        .transpose()?;

    let mut i = 0;
    while i < args.len() {
        if args[i] == "--budget" || args[i] == "--budget-usd" {
            let val = args.get(i + 1).context("--budget requires a value")?;
            budget = Some(val.parse().context("--budget must be a valid float")?);
            i += 1;
        }
        i += 1;
    }
    Ok(budget)
}

pub fn mode_from_args(args: &[String]) -> (Option<String>, bool) {
    let mut mode = None;
    let mut allow_missing_secrets = std::env::var("ALLOW_MISSING_SECRETS")
        .map(|v| v.eq_ignore_ascii_case("true") || v == "1")
        .unwrap_or(false);

    let mut i = 0;
    while i < args.len() {
        if args[i] == "--mode" {
            if let Some(val) = args.get(i + 1) {
                mode = Some(val.clone());
                i += 1;
            }
        } else if args[i] == "--ready" {
            mode = Some("ready".to_string());
        } else if args[i] == "--draft" {
            mode = Some("draft".to_string());
        } else if args[i] == "--allow-missing-secrets" {
            allow_missing_secrets = true;
        }
        i += 1;
    }
    (mode, allow_missing_secrets)
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

// Embedded at build time: remote metadata and cache sidecars cannot change trust.
fn pinned_dataset_digest(dataset: &str, revision: &str) -> Result<String> {
    let pins: HashMap<String, HashMap<String, String>> =
        toml::from_str(include_str!("../../../../evals/pinned-datasets.toml"))?;
    let digest = pins
        .get(dataset)
        .and_then(|revisions| revisions.get(revision))
        .with_context(|| {
            format!("no repo-pinned digest for dataset {dataset} revision {revision}")
        })?;
    if digest.len() != 64 || !digest.bytes().all(|b| b.is_ascii_hexdigit()) {
        bail!("invalid repo-pinned digest for dataset {dataset} revision {revision}");
    }
    Ok(digest.to_ascii_lowercase())
}

fn verify_pinned_digest(bytes: &[u8], expected: &str, source: &str) -> Result<String> {
    let checksum = compute_sha256(bytes);
    if checksum != expected {
        bail!(
            "{source} integrity failure: checksum mismatch against repo-pinned digest (got {checksum}, expected {expected})"
        );
    }
    Ok(checksum)
}

fn hf_outage(error: &reqwest::Error) -> bool {
    if let Some(status) = error.status() {
        return status.is_server_error() || status == reqwest::StatusCode::REQUEST_TIMEOUT;
    }
    error.is_timeout() || error.is_connect() || error.is_request() || error.is_body()
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FetchedDataset {
    pub content: String,
    pub source: String, // "cache", "hf", "mirror", "fixture"
    pub revision: String,
    pub checksum: String,
    pub verified: bool,
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

    let pinned_digest = pinned_dataset_digest(dataset, revision)?;

    std::fs::create_dir_all(cache_dir)?;
    let cache_file = cache_dir.join(format!("{}-{revision}.jsonl", dataset.replace('/', "--")));
    let checksum_file = cache_dir.join(format!("{}-{revision}.sha256", dataset.replace('/', "--")));

    // 1. Check local cache
    if cache_file.is_file() {
        let content = std::fs::read_to_string(&cache_file)
            .with_context(|| format!("failed to read cache file at {}", cache_file.display()))?;
        // A missing sidecar is an incomplete cache, never an opportunity to self-sign.
        let sidecar = std::fs::read_to_string(&checksum_file)
            .context("cache checksum sidecar missing or unreadable; remove cache and refetch from a verified source")?;
        let computed = verify_pinned_digest(content.as_bytes(), &pinned_digest, "cache")?;
        if sidecar.trim() != pinned_digest {
            bail!("cache corruption detected: checksum sidecar does not match repo-pinned digest");
        }

        return Ok(FetchedDataset {
            content,
            source: "cache".to_string(),
            revision: revision.to_string(),
            checksum: computed,
            verified: true,
        });
    }

    // 2. Try primary Hugging Face endpoint
    let hf_url = format!(
        "{}/datasets/{dataset}/resolve/{revision}/golden.jsonl",
        base_url.trim_end_matches('/')
    );
    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(15))
        .build()?;
    let mut req = client.get(&hf_url);
    if !token.is_empty() {
        req = req.bearer_auth(token);
    }

    let hf_result: std::result::Result<Vec<u8>, reqwest::Error> = async {
        let res = req.send().await?.error_for_status()?;
        Ok(res.bytes().await?.to_vec())
    }
    .await;

    match hf_result {
        Ok(bytes) => {
            let checksum = verify_pinned_digest(&bytes, &pinned_digest, "hf")?;
            let body = String::from_utf8(bytes).context("HF dataset is not UTF-8")?;
            std::fs::write(&cache_file, &body)?;
            std::fs::write(&checksum_file, &checksum)?;
            Ok(FetchedDataset {
                content: body,
                source: "hf".to_string(),
                revision: revision.to_string(),
                checksum,
                verified: true,
            })
        }
        Err(hf_err) => {
            if !hf_outage(&hf_err) {
                return Err(hf_err).context(
                    "Hugging Face download failed; mirror fallback only allowed for outages",
                );
            }
            // 3. Fallback to mirror only for primary outages
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
            let mirror_client = client;
            for candidate_url in candidate_urls {
                let resp = match mirror_client.get(&candidate_url).send().await {
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

                let bytes = match resp.bytes().await {
                    Ok(b) => b,
                    Err(e) => {
                        last_mirror_err = Some(e.to_string());
                        continue;
                    }
                };

                let checksum = verify_pinned_digest(&bytes, &pinned_digest, "mirror")?;
                let body =
                    String::from_utf8(bytes.to_vec()).context("mirror dataset is not UTF-8")?;

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

                let mut has_checksum = advertised_checksum.is_some();
                // A checksum header or sidecar is mandatory, but neither supplies trust.
                let checksum_url = format!("{candidate_url}.sha256");
                if let Ok(chk_resp) = mirror_client.get(&checksum_url).send().await
                    && chk_resp.status().is_success()
                    && let Ok(chk_text) = chk_resp.text().await
                {
                    let expected = chk_text.trim();
                    has_checksum |= !expected.is_empty();
                    if !expected.is_empty() && expected != checksum {
                        bail!(
                            "mirror integrity failure: checksum mismatch for mirror dataset at {candidate_url} (computed {checksum}, expected in .sha256 {expected})"
                        );
                    }
                }

                if !has_checksum {
                    bail!("mirror integrity failure: missing checksum for {candidate_url}");
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
                    verified: true,
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
            usage_unavailable: false,
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
    let outcome = engine_core::credits::run(
        &router,
        "preview",
        "fixture",
        &limits,
        engine_core::courses::validate_preview,
    )
    .await;
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

/// Cold: the provider only wrote the cache; warm: it read it; none: no cache tokens.
fn cache_class(usage: &serde_json::Value) -> &'static str {
    let n = |k: &str| usage[k].as_u64().unwrap_or(0);
    if n("cache_read_tokens") > 0 {
        "warm"
    } else if n("cache_write_tokens") > 0 {
        "cold"
    } else {
        "none"
    }
}

/// Runs the production tutor prompt builder on the case context and scores a recorded reply.
fn evaluate_tutor_chat(input: &serde_json::Value) -> Result<String> {
    use engine_core::chat;
    let context: chat::Context = serde_json::from_value(input["context"].clone())?;
    let prompt = chat::prompt(&context);
    let strings = |key: &str| -> Vec<String> {
        input[key]
            .as_array()
            .into_iter()
            .flatten()
            .filter_map(|v| v.as_str().map(str::to_owned))
            .collect()
    };
    let reply = input["recorded"][0].as_str().context("recorded reply")?;
    let parts = chat::split(&prompt);
    let data = parts.and_then(|(_, json, _)| serde_json::from_str::<serde_json::Value>(json).ok());
    let instructions_clean = parts.is_some_and(|(before, _, after)| {
        strings("markers")
            .iter()
            .all(|m| !before.contains(m.as_str()) && !after.contains(m.as_str()))
    });
    let history: Vec<String> = data
        .as_ref()
        .and_then(|d| serde_json::from_value(d["history"].clone()).ok())
        .unwrap_or_default();
    Ok(serde_json::json!({
        "prompt_valid": data.is_some(),
        "delimiter_intact": prompt.matches(chat::OPEN).count() == 1 && prompt.matches(chat::CLOSE).count() == 1,
        "instructions_clean": instructions_clean,
        "profile_provided": data.as_ref().is_some_and(|d| d["profile_provided"] == true),
        "current_lesson_empty": data.as_ref().is_some_and(|d| d["current_lesson"].is_null()),
        "history_oldest_first": history == context.history,
        "excerpts_in_data_block": !context.excerpts.is_empty()
            && data.as_ref().is_some_and(|d| {
                let found = d["source_excerpts"].as_array().map_or(0, Vec::len);
                found == context.excerpts.len()
                    && strings("excerpt_terms").iter().all(|t| {
                        d["source_excerpts"]
                            .to_string()
                            .contains(t.trim_matches('"'))
                    })
            }),
        "excerpts_outside_instructions": parts.is_some_and(|(before, _, after)| {
            context.excerpts.iter().all(|e| {
                !before.contains(e.text.as_str()) && !after.contains(e.text.as_str())
            })
        }),
        "every_summary_kept": data
            .as_ref()
            .is_some_and(|d| d["history"].as_array().map_or(0, Vec::len) == context.history.len()),
        "cache_class": cache_class(&input["usage"]),
        "envelope": input["envelope"].as_str().unwrap_or("short"),
        "grounded": strings("expect_terms").iter().all(|t| reply.contains(t.as_str())),
        "forbidden_absent": strings("forbidden").iter().all(|t| !reply.contains(t.as_str())),
    })
    .to_string())
}

/// One scripted provider for tutor streaming cases: emits `deltas`, then ends, fails or hangs.
struct ScriptedStream {
    name: &'static str,
    deltas: Vec<String>,
    then: String,
    touched: Arc<std::sync::atomic::AtomicBool>,
    dropped: Arc<std::sync::atomic::AtomicBool>,
}

struct DropFlag(Arc<std::sync::atomic::AtomicBool>);
impl Drop for DropFlag {
    fn drop(&mut self) {
        self.0.store(true, std::sync::atomic::Ordering::SeqCst);
    }
}

#[async_trait::async_trait]
impl engine_core::LlmProvider for ScriptedStream {
    fn name(&self) -> &'static str {
        self.name
    }
    async fn complete(&self, _: &str, _: &str) -> Result<engine_core::Completion> {
        bail!("tutor stream cases never call complete")
    }
    async fn stream(&self, _: &str, _: &str) -> Result<engine_core::CompletionStream> {
        use futures::StreamExt;
        self.touched
            .store(true, std::sync::atomic::Ordering::SeqCst);
        let mut items: Vec<Result<engine_core::StreamEvent>> = self
            .deltas
            .iter()
            .map(|d| Ok(engine_core::StreamEvent::Delta(d.clone())))
            .collect();
        if self.then == "fail" {
            items.push(Err(anyhow::anyhow!("provider reset")));
        }
        let head = futures::stream::iter(items);
        let guard = DropFlag(self.dropped.clone());
        let tail: engine_core::CompletionStream = if self.then == "hang" {
            Box::pin(head.chain(futures::stream::pending()))
        } else {
            Box::pin(head)
        };
        Ok(Box::pin(tail.map(move |item| {
            let _keep = &guard;
            item
        })))
    }
}

/// Runs the production stream path (`stream_bounded_routed` + `reconcile`) on a scripted provider and
/// reports what the API would see: delivered text, terminal state, typed error, usage and fallback facts.
async fn evaluate_tutor_stream(input: &serde_json::Value) -> Result<String> {
    use engine_core::chat::stream::{Bounds, Part, reconcile};
    use futures::StreamExt;
    use std::sync::atomic::{AtomicBool, Ordering};
    let spec = &input["stream"];
    let side = |key: &str, name: &'static str| {
        let s = &spec[key];
        ScriptedStream {
            name,
            deltas: s["deltas"]
                .as_array()
                .into_iter()
                .flatten()
                .filter_map(|v| v.as_str().map(str::to_owned))
                .collect(),
            then: s["then"].as_str().unwrap_or("end").into(),
            touched: Arc::new(AtomicBool::new(false)),
            dropped: Arc::new(AtomicBool::new(false)),
        }
    };
    let (primary, fallback) = (side("primary", "primary"), side("fallback", "fallback"));
    let (primary_dropped, fallback_touched) = (primary.dropped.clone(), fallback.touched.clone());
    let router = engine_core::ProviderRouter::new(Arc::new(primary), Arc::new(fallback))?;
    let ms = |key: &str, default: u64| spec[key].as_u64().unwrap_or(default);
    let bounds = Bounds {
        input_tokens: u32::try_from(ms("input_tokens", 4000))?,
        output_tokens: u32::try_from(ms("output_tokens", 64))?,
    };
    let (stream, route) = engine_core::eval::stream_bounded_routed(
        &router,
        "chat",
        "prompt",
        None,
        std::time::Duration::from_millis(ms("first_token_ms", 40)),
        std::time::Duration::from_millis(ms("idle_ms", 40)),
    )
    .await?;
    let mut parts = Box::pin(reconcile(stream, route.clone(), 400, Some(bounds)));
    let cancel_after = spec["cancel_after"].as_u64();
    let (mut text, mut seen, mut cancelled) = (String::new(), 0u64, false);
    let (mut error, mut usages) = (None, Vec::new());
    while let Some(part) = parts.next().await {
        match part {
            Part::Delta(d) => {
                text.push_str(&d);
                seen += 1;
                if cancel_after.is_some_and(|n| seen >= n) {
                    cancelled = true;
                    break;
                }
            }
            Part::Usage {
                completion,
                estimated,
            } => usages.push((completion, estimated)),
            Part::Error(f) => error = Some(f),
        }
    }
    drop(parts);
    tokio::task::yield_now().await;
    let within = usages.iter().all(|(c, _)| {
        c.input_tokens <= bounds.input_tokens && c.output_tokens <= bounds.output_tokens
    });
    let primary_delivered = !spec["primary"]["deltas"]
        .as_array()
        .is_none_or(|d| d.is_empty());
    Ok(serde_json::json!({
        "delivered_text": text,
        "terminal": if cancelled { "cancelled" } else if error.is_some() { "error" } else { "done" },
        "error_code": error.as_ref().map(|f| f.code).unwrap_or(""),
        "retryable": error.as_ref().is_some_and(|f| f.retryable),
        "fallback_used": route.fallback_used,
        "second_provider_after_delta": primary_delivered && fallback_touched.load(Ordering::SeqCst),
        "usage_frames": usages.len(),
        "usage_estimated": usages.iter().any(|(_, e)| *e),
        "usage_within_bounds": within,
        "provider_dropped": primary_dropped.load(Ordering::SeqCst),
    })
    .to_string())
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

async fn evaluate_retry_variants(input: &serde_json::Value) -> Result<String> {
    use engine_core::lessontest::retry;
    let source: retry::Question = serde_json::from_value(input["source"].clone())?;
    if retry::validate_source(&source).is_err() {
        return Ok(
            serde_json::json!({"delivered":false,"attempt_count":0,"variants":[]}).to_string(),
        );
    }
    let previous: Vec<retry::Question> = serde_json::from_value(
        input
            .get("previous_variants")
            .cloned()
            .unwrap_or_else(|| serde_json::json!([])),
    )?;
    let recorded: Vec<Option<engine_core::Completion>> =
        serde_json::from_value(input["recorded"].clone())?;
    let provider = Arc::new(RecordedCredits(Mutex::new(recorded.into())));
    let router = engine_core::ProviderRouter::new(provider.clone(), provider)?;
    let limits: engine_core::credits::Limits = serde_json::from_value(input["limits"].clone())?;
    let outcome = retry::generate(&router, &source, &previous, &limits).await?;
    let variants = outcome
        .completion
        .as_ref()
        .and_then(|c| retry::parse_variants(&source, &previous, &c.text).ok())
        .unwrap_or_default();
    Ok(serde_json::json!({"delivered":!variants.is_empty(),"variants":variants,"attempt_count":outcome.attempts.len()}).to_string())
}

/// Offline persona/injection case: persona text is bounded data below trusted
/// Course rules, segments only use pinned speakers, retries reuse the snapshot.
fn evaluate_persona_lesson(input: &serde_json::Value) -> Result<String> {
    let cast = input["cast"].to_string();
    let course_rule = input["course_rule"].as_str().unwrap_or_default();
    let personas = engine_core::characters::lesson_personas(&cast);
    let prompt = format!("{course_rule}\n{}", personas.as_deref().unwrap_or_default());
    let segments_valid =
        engine_core::characters::validate_lesson_segments(&input["lesson"], &cast).is_ok();
    let retry_cast = input.get("retry_cast").unwrap_or(&input["cast"]);
    let leaked = input["lesson"]
        .to_string()
        .to_lowercase()
        .contains("system prompt");
    Ok(serde_json::json!({
        "personas_valid": personas.is_ok(),
        "data_only": personas.as_ref().is_ok_and(|p| p.contains("style only") && !p.contains('<') && !p.contains('>')),
        "course_rule_first": course_rule.is_empty() || prompt.starts_with(course_rule),
        "segments_valid": segments_valid,
        "snapshot_immutable": retry_cast == &input["cast"],
        "grading_unchanged": input["lesson"].get("test").is_none() || input["lesson"]["test"] == input["expected_test"],
        "prompt_leak": leaked,
    })
    .to_string())
}

/// Offline reaction case: the reaction depends only on the authoritative graded
/// result; persona/injection text never reaches reaction copy or grading state.
fn evaluate_reaction(input: &serde_json::Value) -> Result<String> {
    let flag = |k: &str| input[k].as_bool().unwrap_or(false);
    let r =
        engine_core::characters::reaction_for(flag("correct"), flag("passed"), flag("has_retry"));
    let out = serde_json::json!({"trigger": r.trigger, "expression": r.expression, "copy": r.copy})
        .to_string();
    let persona = input["persona_text"]
        .as_str()
        .unwrap_or_default()
        .to_lowercase();
    Ok(serde_json::json!({
        "trigger": r.trigger,
        "expression": r.expression,
        "copy_approved": engine_core::characters::reaction_copy_approved(r.copy),
        "persona_in_output": !persona.is_empty() && out.to_lowercase().contains(&persona),
        "grading_unchanged": input["grading_after"] == input["grading_before"],
        "state_immutable": input["state_after"] == input["state_before"],
        "answer_hidden": !input["presubmit_payload"].to_string().contains("answer_index"),
        "prompt_leak": out.to_lowercase().contains("system prompt"),
    })
    .to_string())
}

async fn evaluate_character(task: &str, input: &serde_json::Value) -> Result<String> {
    if task == "character_reactions" || input.get("reaction_case").is_some() {
        return evaluate_reaction(input);
    }
    if input.get("cast").is_some() {
        return evaluate_persona_lesson(input);
    }
    let spec = input["spec"].to_string();
    let valid = engine_core::characters::valid_spec(&spec);
    if task == "character_spec" && input["mapping"] == true {
        let recorded: Vec<Option<engine_core::Completion>> =
            serde_json::from_value(input["recorded"].clone())?;
        let provider = Arc::new(RecordedCredits(Mutex::new(recorded.into())));
        let router = engine_core::ProviderRouter::new(provider.clone(), provider)?;
        let limits = serde_json::from_value(input["limits"].clone())?;
        let out = engine_core::characters::map_character(
            &router,
            input["prompt"].as_str().unwrap_or_default(),
            &limits,
        )
        .await?;
        return Ok(serde_json::json!({"valid":out.completion.as_ref().is_some_and(|c|engine_core::characters::valid_spec(&c.text)),"attempt_count":out.attempts.len(),"fallback_used":out.attempts.iter().any(|a|a.fallback_used),"delivered_count":out.attempts.iter().filter(|a|a.delivered).count()}).to_string());
    }
    if task == "character_spec" {
        return Ok(serde_json::json!({"valid":valid}).to_string());
    }
    if !valid {
        return Ok(
            serde_json::json!({"approved":false,"reason_code":"invalid_spec","attempt_count":0})
                .to_string(),
        );
    }
    let recorded: Vec<Option<engine_core::Completion>> =
        serde_json::from_value(input["recorded"].clone())?;
    let provider = Arc::new(RecordedCredits(Mutex::new(recorded.into())));
    let router = engine_core::ProviderRouter::new(provider.clone(), provider)?;
    let outcome = engine_core::characters::screen(&router, &spec, "").await?;
    let (approved, reason) = outcome
        .completion
        .as_ref()
        .and_then(|c| engine_core::characters::verdict(&c.text))
        .unwrap_or((false, "screening_unavailable".into()));
    Ok(serde_json::json!({"approved":approved,"reason_code":reason,"attempt_count":outcome.attempts.len()}).to_string())
}

async fn evaluate_note_enrich(input: &serde_json::Value) -> Result<String> {
    let recorded: Vec<Option<engine_core::Completion>> =
        serde_json::from_value(input["recorded"].clone())?;
    let provider = Arc::new(RecordedCredits(Mutex::new(recorded.into())));
    let router = engine_core::ProviderRouter::new(provider.clone(), provider)?;
    let limits: engine_core::credits::Limits = serde_json::from_value(input["limits"].clone())?;
    let note = input["note"].as_str().unwrap_or_default();
    let lesson: Option<engine_core::notes::LessonRef> = input
        .get("lesson")
        .and_then(|l| serde_json::from_value(l.clone()).ok());
    let learner: Option<engine_core::notes::LearnerProfile> = input
        .get("learner")
        .and_then(|p| serde_json::from_value(p.clone()).ok());
    let allowed_lessons: Vec<engine_core::notes::LessonRef> = input
        .get("allowed_lessons")
        .and_then(|a| serde_json::from_value(a.clone()).ok())
        .unwrap_or_default();

    let outcome = engine_core::notes::enrich(
        &router,
        note,
        lesson.as_ref(),
        learner.as_ref(),
        &allowed_lessons,
        &limits,
    )
    .await?;

    let res = outcome.result;
    let mut val = if let Some(r) = res {
        serde_json::json!({
            "delivered": true,
            "summary": r.summary,
            "tags": r.tags,
            "links": r.links,
        })
    } else {
        serde_json::json!({
            "delivered": false,
            "summary": "",
            "tags": [],
            "links": [],
        })
    };
    val["attempt_count"] = serde_json::json!(outcome.attempts.len());
    val["fallback_used"] = serde_json::json!(outcome.fallback_used);
    Ok(val.to_string())
}

#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub struct ModelPrice {
    #[serde(default)]
    pub input_nanos: u64,
    #[serde(default)]
    pub output_nanos: u64,
    #[serde(default)]
    pub cache_write_nanos: u64,
    #[serde(default)]
    pub cache_read_nanos: u64,
}

#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub struct PriceTable {
    pub models: HashMap<String, ModelPrice>,
}

impl PriceTable {
    pub fn load() -> Self {
        if let Ok(path) = std::env::var("EVAL_PRICES_CONFIG")
            && let Ok(content) = std::fs::read_to_string(&path)
            && let Ok(pt) = toml::from_str(&content)
        {
            return pt;
        }
        toml::from_str(include_str!("../../../config/prices.toml"))
            .expect("valid embedded prices.toml")
    }

    pub fn price_for(&self, model: &str) -> ModelPrice {
        if let Some(p) = self.models.get(model) {
            return p.clone();
        }
        self.models.get("default").cloned().unwrap_or(ModelPrice {
            input_nanos: 2000,
            output_nanos: 10000,
            cache_write_nanos: 2500,
            cache_read_nanos: 100,
        })
    }

    pub fn cost_usd(&self, c: &engine_core::Completion) -> f64 {
        let p = self.price_for(&c.model);
        let nanos = (c.input_tokens as u64 * p.input_nanos)
            + (c.output_tokens as u64 * p.output_nanos)
            + (c.cache_write_tokens as u64 * p.cache_write_nanos)
            + (c.cache_read_tokens as u64 * p.cache_read_nanos);
        nanos as f64 / 1_000_000_000.0
    }
}

pub struct Runner {
    pub provider_name: String,
    pub judge: bool,
    pub dataset_name: String,
    pub dataset_revision: String,
    pub dataset_source: String,
    pub dataset_verified: bool,
    pub prompt_revision: String,
    pub judge_fixtures: HashMap<String, String>,
    pub budget_usd: Option<f64>,
    pub total_cost_usd: f64,
    pub budget_exceeded: bool,
    pub custom_router: Option<engine_core::ProviderRouter>,
}

impl Runner {
    pub fn new(provider_name: &str, judge: bool) -> Self {
        Self {
            provider_name: provider_name.to_string(),
            judge,
            dataset_name: "datamonsterr/duolify-golden".to_string(),
            dataset_revision: "pinned".to_string(),
            dataset_source: "fixture".to_string(),
            dataset_verified: false,
            prompt_revision: "head".to_string(),
            judge_fixtures: HashMap::new(),
            budget_usd: None,
            total_cost_usd: 0.0,
            budget_exceeded: false,
            custom_router: None,
        }
    }

    pub fn with_router(
        router: engine_core::ProviderRouter,
        provider_name: &str,
        judge: bool,
    ) -> Self {
        Self {
            provider_name: provider_name.to_string(),
            judge,
            dataset_name: "datamonsterr/duolify-golden".to_string(),
            dataset_revision: "pinned".to_string(),
            dataset_source: "fixture".to_string(),
            dataset_verified: false,
            prompt_revision: "head".to_string(),
            judge_fixtures: HashMap::new(),
            budget_usd: None,
            total_cost_usd: 0.0,
            budget_exceeded: false,
            custom_router: Some(router),
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
        let router = match &self.custom_router {
            Some(r) => r.clone(),
            None => engine_providers::router(&self.provider_name)?,
        };
        let price_table = PriceTable::load();
        let mut results = Vec::new();
        let mut models = HashMap::new();
        let mut total_cost_usd = 0.0;
        let mut budget_exceeded = false;
        let mut any_critical_failed = false;

        for case in &cases {
            if budget_exceeded {
                break;
            }
            let canonical = canonical_task_name(&case.task);
            let route = model_route_for_task(&case.task);
            models
                .entry(canonical.clone())
                .or_insert_with(|| route.clone());

            let mut _attempts = Vec::new();
            let mut primary_completion: Option<engine_core::Completion> = None;

            let response_text = if matches!(
                case.task.as_str(),
                "character_spec"
                    | "character_screen"
                    | "character_injection"
                    | "character_persona_lesson"
                    | "character_reactions"
            ) && self.provider_name == "mock"
                && self.custom_router.is_none()
            {
                evaluate_character(&case.task, &case.input).await?
            } else if case.input.get("recorded").is_some()
                && self.provider_name == "mock"
                && self.custom_router.is_none()
            {
                if canonical == "lesson_test_variant" {
                    evaluate_retry_variants(&case.input).await?
                } else if canonical == "course_outline" {
                    evaluate_course_outline(&case.input).await?
                } else if canonical == "course_preview" {
                    evaluate_preview(&case.input).await?
                } else if case.task == "outline_chat_change" {
                    evaluate_outline_change(&case.input).await?
                } else if case.task == "credit_envelope" {
                    evaluate_credits(&case.input).await?
                } else if case.task == "course_brief_chat" {
                    evaluate_brief(&case.input).await?
                } else if case.task == "tutor_chat" && case.input.get("stream").is_some() {
                    evaluate_tutor_stream(&case.input).await?
                } else if case.task == "tutor_chat" {
                    evaluate_tutor_chat(&case.input)?
                } else if canonical == "note_enrich" {
                    evaluate_note_enrich(&case.input).await?
                } else {
                    let limits = case
                        .input
                        .get("limits")
                        .and_then(|l| {
                            serde_json::from_value::<engine_core::credits::Limits>(l.clone()).ok()
                        })
                        .unwrap_or(engine_core::credits::Limits {
                            input_tokens: 16000,
                            output_tokens: 4000,
                            timeout_ms: 30000,
                            max_attempts: 2,
                            max_cost_nanos: 1_000_000_000,
                            input_nanos: 2000,
                            output_nanos: 10000,
                            cache_write_nanos: 2500,
                            cache_read_nanos: 100,
                            deadline_unix_ms: 0,
                        });
                    let outcome = engine_core::eval::run_bounded(
                        &router,
                        &route,
                        &case.input.to_string(),
                        &limits,
                        |_| true,
                    )
                    .await?;
                    _attempts = outcome.attempts;
                    let c = outcome
                        .completion
                        .context("case execution produced no completion")?;
                    let text = c.text.clone();
                    primary_completion = Some(c);
                    text
                }
            } else if canonical == "course_outline"
                && self.provider_name == "mock"
                && self.custom_router.is_none()
                && case.input.get("outline").is_some()
            {
                evaluate_course_outline(&case.input).await?
            } else {
                let limits = case
                    .input
                    .get("limits")
                    .and_then(|l| {
                        serde_json::from_value::<engine_core::credits::Limits>(l.clone()).ok()
                    })
                    .unwrap_or(engine_core::credits::Limits {
                        input_tokens: 16000,
                        output_tokens: 4000,
                        timeout_ms: 30000,
                        max_attempts: 2,
                        max_cost_nanos: 1_000_000_000,
                        input_nanos: 2000,
                        output_nanos: 10000,
                        cache_write_nanos: 2500,
                        cache_read_nanos: 100,
                        deadline_unix_ms: 0,
                    });
                let outcome = engine_core::eval::run_bounded(
                    &router,
                    &route,
                    &case.input.to_string(),
                    &limits,
                    |_| true,
                )
                .await?;
                _attempts = outcome.attempts;
                let c = outcome
                    .completion
                    .context("case execution produced no completion")?;
                let text = c.text.clone();
                primary_completion = Some(c);
                text
            };

            let mut errors = Vec::new();
            let mut critical_failed = false;

            let (mut input_tokens, mut output_tokens, mut case_cost_usd) =
                if self.provider_name != "mock" || self.custom_router.is_some() {
                    let (in_tok, out_tok, cost) = if let Some(c) = &primary_completion {
                        if c.usage_unavailable {
                            errors.push(
                                "usage_unavailable: provider usage missing or unmeasured".into(),
                            );
                            critical_failed = true;
                        }
                        (c.input_tokens, c.output_tokens, price_table.cost_usd(c))
                    } else {
                        (0, 0, 0.0)
                    };
                    (in_tok, out_tok, cost)
                } else if self.budget_usd.is_none()
                    && case.input.get("cost_usd").is_none()
                    && case.input.get("cost_nanos").is_none()
                {
                    (0, 0, 0.0)
                } else if let Some(cost) = case.input.get("cost_usd").and_then(|v| v.as_f64()) {
                    (100, 50, cost)
                } else if let Some(nanos) = case.input.get("cost_nanos").and_then(|v| v.as_f64()) {
                    (100, 50, nanos / 1_000_000_000.0)
                } else {
                    let in_tok = case
                        .input
                        .get("recorded")
                        .and_then(|r| r.get(0))
                        .and_then(|c| c.get("input_tokens"))
                        .and_then(|n| n.as_u64())
                        .map(|n| n as u32)
                        .unwrap_or_else(|| (case.input.to_string().len() / 4).max(1) as u32);
                    let out_tok = case
                        .input
                        .get("recorded")
                        .and_then(|r| r.get(0))
                        .and_then(|c| c.get("output_tokens"))
                        .and_then(|n| n.as_u64())
                        .map(|n| n as u32)
                        .unwrap_or_else(|| (response_text.len() / 4).max(1) as u32);

                    let in_nanos = case
                        .input
                        .get("limits")
                        .and_then(|l| l.get("input_nanos"))
                        .and_then(|n| n.as_u64())
                        .unwrap_or(2000);
                    let out_nanos = case
                        .input
                        .get("limits")
                        .and_then(|l| l.get("output_nanos"))
                        .and_then(|n| n.as_u64())
                        .unwrap_or(10000);

                    let cost_nanos = (in_tok as u64 * in_nanos) + (out_tok as u64 * out_nanos);
                    (in_tok, out_tok, cost_nanos as f64 / 1_000_000_000.0)
                };

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

            let judge_passed = if self.judge {
                let (verdict_str, judge_comp) = if let Some(preset) =
                    self.judge_fixtures.get(&case.id)
                {
                    (preset.clone(), None)
                } else if self.provider_name == "mock" && self.custom_router.is_none() {
                    // In mock mode without an explicit calibration preset, default to PASS
                    ("PASS".to_string(), None)
                } else {
                    let judge_prompt = format!(
                        "Judge this response against the rubric. Reply PASS or FAIL. Rubric: {}\nResponse: {}",
                        case.rubric, response_text
                    );
                    let judge_limits = engine_core::credits::Limits {
                        input_tokens: 8000,
                        output_tokens: 1000,
                        timeout_ms: 30000,
                        max_attempts: 2,
                        max_cost_nanos: 100_000_000,
                        input_nanos: 1000,
                        output_nanos: 5000,
                        cache_write_nanos: 1250,
                        cache_read_nanos: 100,
                        deadline_unix_ms: 0,
                    };
                    let outcome = engine_core::eval::run_bounded(
                        &router,
                        "chat",
                        &judge_prompt,
                        &judge_limits,
                        |_| true,
                    )
                    .await?;
                    let comp = outcome.completion.context("judge produced no completion")?;
                    let text = comp.text.clone();
                    (text, Some(comp))
                };

                if let Some(jc) = &judge_comp {
                    if jc.usage_unavailable {
                        errors.push("usage_unavailable: judge usage missing or unmeasured".into());
                        critical_failed = true;
                    }
                    input_tokens += jc.input_tokens;
                    output_tokens += jc.output_tokens;
                    case_cost_usd += price_table.cost_usd(jc);
                }

                let pass = parse_judge_verdict(&verdict_str).unwrap_or(false);
                if !pass {
                    errors.push("judge check failed".into());
                }
                Some(pass)
            } else {
                None
            };

            total_cost_usd += case_cost_usd;

            let mut case_budget_exceeded = false;
            if let Some(budget) = self.budget_usd
                && total_cost_usd >= budget
            {
                budget_exceeded = true;
                case_budget_exceeded = true;
                errors.push("eval budget exceeded".into());
            }

            if critical_failed {
                any_critical_failed = true;
            }

            let deterministic_passed = errors.is_empty();
            let passed = deterministic_passed
                && judge_passed.unwrap_or(true)
                && !case_budget_exceeded
                && !critical_failed;

            results.push(CaseResult {
                id: case.id.clone(),
                task: canonical,
                passed,
                critical_failed,
                deterministic_passed,
                judge_passed,
                errors,
                cost_usd: case_cost_usd,
                input_tokens,
                output_tokens,
            });

            if budget_exceeded {
                break;
            }
        }

        let (task_reports, overall_passed, critical_failure) =
            aggregate_task_reports(&results, thresholds, &models);

        let final_critical = critical_failure || any_critical_failed;
        let final_passed = overall_passed && !budget_exceeded && !final_critical;

        let status = if budget_exceeded {
            "budget_exceeded".to_string()
        } else if final_critical {
            "critical_failure".to_string()
        } else if final_passed {
            "passed".to_string()
        } else {
            "failed".to_string()
        };

        let stamp = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_millis();

        let mut report = EvalReport {
            timestamp_millis: stamp,
            dataset: self.dataset_name.clone(),
            dataset_revision: self.dataset_revision.clone(),
            dataset_source: self.dataset_source.clone(),
            dataset_verified: self.dataset_verified,
            prompt_revision: self.prompt_revision.clone(),
            provider: self.provider_name.clone(),
            budget_usd: self.budget_usd,
            total_cost_usd,
            budget_exceeded,
            status,
            live_promoted: false,
            passed: final_passed,
            critical_failure: final_critical,
            task_reports,
            results,
        };

        report.live_promoted = check_promotion_gate(&report).is_ok();

        Ok(report)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn credit_fixture_retries_valid_json_with_invalid_preview_schema() {
        let input = serde_json::json!({
            "recorded": [
                {"text":"{}","provider":"recorded","model":"fixture","input_tokens":0,"output_tokens":0},
                {"text":"{\"title\":\"Sample\",\"units\":[{\"title\":\"Unit\",\"lessons\":[{\"title\":\"Lesson\",\"goal\":\"Learn\"}]}],\"html\":\"<div id=root>Example</div>\",\"narration\":\"Example\"}","provider":"recorded","model":"fixture","input_tokens":0,"output_tokens":0}
            ],
            "limits": {"input_tokens":2000,"output_tokens":2500,"timeout_ms":3000,"max_attempts":2,"max_cost_nanos":60000000,"input_nanos":2000,"output_nanos":10000,"cache_write_nanos":2500,"cache_read_nanos":100}
        });
        let result: serde_json::Value =
            serde_json::from_str(&evaluate_credits(&input).await.unwrap()).unwrap();
        assert_eq!(result["delivered"], true);
        assert_eq!(result["attempt_count"], 2);
        assert_eq!(result["attempts"][0]["delivered"], false);
    }
}
