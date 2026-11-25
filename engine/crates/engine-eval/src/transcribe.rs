//! `transcribe` golden cases (voice input, openspec change immersive-mobile-lesson-player 6.7).
//!
//! A live case names one recording stored beside the dataset (`audio.file`, pinned by `audio.sha256`
//! inside the digest-pinned `golden.jsonl`), runs it through the production `Transcribe` path
//! (`engine_core::transcribe::transcribe`: primary, fallback, timeouts, cost ceiling, silence
//! filter) and scores word error rate, silence, latency and cost against the case's `thresholds`.
//! An offline case replays recorded provider answers through the same production path.
//! Neither the recording nor the transcript is written to the report: only numbers and flags.
use anyhow::{Context, Result, bail};
use async_trait::async_trait;
use engine_core::{
    Completion, CompletionStream, LlmProvider, ProviderRouter, Route,
    transcribe::{self as stt, Recording, Transcription},
};
use serde_json::{Value, json};
use std::{
    path::PathBuf,
    sync::{
        Arc,
        atomic::{AtomicU32, Ordering},
    },
};

/// Provider name reported for transcription attempts.
pub const PROVIDER: &str = "openrouter";

/// The scored JSON text of a case and what the provider billed for it.
pub struct Scored {
    pub text: String,
    pub cost_usd: f64,
    /// One entry per delivered answer (model, latency, fallback flag), for the report.
    pub attempts: Vec<engine_core::credits::Attempt>,
}

fn thresholds(input: &Value) -> (f64, u32, f64) {
    let t = &input["thresholds"];
    (
        t["max_wer"].as_f64().unwrap_or(0.15),
        t["max_latency_ms"].as_u64().unwrap_or(6000) as u32,
        t["max_cost_usd"]
            .as_f64()
            .unwrap_or(stt::MAX_COST_NANOS as f64 / 1e9),
    )
}

/// Scores a delivered transcript. `reference` empty means silence or noise only: any word is a
/// failure ("empty text" rule), and WER is not defined.
fn score(
    input: &Value,
    outcome: &Result<stt::Outcome, stt::TranscribeError>,
    calls: u32,
) -> Scored {
    let reference = input["reference"].as_str().unwrap_or_default();
    let silence_case = stt::normalized(reference).is_empty();
    let (max_wer, max_latency_ms, max_cost_usd) = thresholds(input);
    let body = match outcome {
        Ok(o) => {
            let heard = &o.transcription.text;
            let wer = stt::wer(reference, heard);
            let cost_usd = o.transcription.cost_nanos as f64 / 1e9;
            json!({
                "delivered": true,
                "error": "",
                "category": input["category"],
                "silence_case": silence_case,
                "empty": stt::normalized(heard).is_empty(),
                "wer": wer,
                "wer_within": wer.is_some_and(|w| w <= max_wer),
                "silence_ok": silence_case && stt::normalized(heard).is_empty(),
                "latency_ms": o.latency_ms,
                "latency_within": o.latency_ms <= max_latency_ms,
                "cost_usd": cost_usd,
                "cost_within": cost_usd <= max_cost_usd,
                "fallback_used": o.fallback_used,
                "attempts": o.attempts,
                "model": o.transcription.model,
                "language": o.transcription.language,
                "provider_calls": calls,
            })
        }
        Err(e) => json!({
            "delivered": false,
            "error": e.reason(),
            "category": input["category"],
            "silence_case": silence_case,
            "provider_calls": calls,
        }),
    };
    let cost_usd = outcome
        .as_ref()
        .map_or(0.0, |o| o.transcription.cost_nanos as f64 / 1e9);
    let attempts = outcome
        .as_ref()
        .map(|o| engine_core::credits::Attempt {
            provider: crate::transcribe::PROVIDER.into(),
            model: o.transcription.model.clone(),
            latency_ms: o.latency_ms,
            attempt: o.attempts,
            fallback_used: o.fallback_used,
            delivered: true,
            input_tokens: 0,
            output_tokens: 0,
            cache_write_tokens: 0,
            cache_read_tokens: 0,
            usage_unavailable: false,
            worst_case_nanos: 0,
            error_code: None,
        })
        .into_iter()
        .collect();
    Scored {
        text: body.to_string(),
        cost_usd,
        attempts,
    }
}

fn recording<'a>(input: &'a Value, audio: &'a [u8]) -> Recording<'a> {
    Recording {
        audio,
        container: input["audio"]["container"].as_str().unwrap_or_default(),
        duration_ms: input["audio"]["duration_ms"].as_u64().unwrap_or(0) as u32,
        language_hint: "",
    }
}

fn cache_dir() -> PathBuf {
    std::env::var("EVAL_AUDIO_DIR").map_or_else(
        |_| PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../../evals/.cache/audio"),
        PathBuf::from,
    )
}

/// Reads the case's recording, from the local cache or from the dataset revision on Hugging Face,
/// and refuses bytes that do not match the digest the pinned `golden.jsonl` carries.
async fn load_audio(input: &Value, dataset: &str, revision: &str) -> Result<Vec<u8>> {
    let file = input["audio"]["file"].as_str().context("audio.file")?;
    let digest = input["audio"]["sha256"].as_str().context("audio.sha256")?;
    if digest.len() != 64 || !digest.bytes().all(|b| b.is_ascii_hexdigit()) {
        bail!("audio.sha256 must be 64 hex characters");
    }
    let cached = cache_dir().join(digest);
    if let Ok(bytes) = std::fs::read(&cached)
        && crate::compute_sha256(&bytes) == digest
    {
        return Ok(bytes);
    }
    if !crate::is_pinned_commit(revision) {
        bail!("audio is fetched only from a pinned 40-character dataset revision");
    }
    let bytes = if let Ok(token) = std::env::var("HF_TOKEN")
        && !token.is_empty()
    {
        let url = format!("https://huggingface.co/datasets/{dataset}/resolve/{revision}/{file}");
        let response = reqwest::Client::new()
            .get(url)
            .bearer_auth(token)
            .send()
            .await?
            .error_for_status()
            .context("audio download")?;
        response.bytes().await?.to_vec()
    } else {
        // The logged-in `hf` CLI downloads it; no token is read or printed here.
        let scratch = cache_dir().join(format!(".dl-{digest}"));
        let status = tokio::process::Command::new("hf")
            .args([
                "download",
                dataset,
                file,
                "--repo-type",
                "dataset",
                "--revision",
                revision,
            ])
            .arg("--local-dir")
            .arg(&scratch)
            .stdout(std::process::Stdio::null())
            .status()
            .await
            .context("run `hf download` (install the hf CLI or set HF_TOKEN)")?;
        if !status.success() {
            bail!("hf download of {file} failed");
        }
        let bytes = std::fs::read(scratch.join(file))?;
        let _ = std::fs::remove_dir_all(&scratch);
        bytes
    };
    if crate::compute_sha256(&bytes) != digest {
        bail!("audio {file} does not match its pinned sha256");
    }
    std::fs::create_dir_all(cache_dir())?;
    std::fs::write(&cached, &bytes)?;
    Ok(bytes)
}

/// Live case: the production `transcribe` on the live router. Setup problems (missing audio,
/// digest mismatch) are errors, not scores.
pub async fn live(
    input: &Value,
    router: &ProviderRouter,
    dataset: &str,
    revision: &str,
) -> Result<Scored> {
    let audio = load_audio(input, dataset, revision).await?;
    // `route: "fallback"` skips the primary (it always fails) so the fallback model is measured too.
    let forced = (input["route"] == "fallback").then(|| {
        ProviderRouter::new(Arc::new(Recorded::failing()), router.fallback.clone())
            .expect("the embedded models.toml parses")
    });
    let outcome =
        stt::transcribe(forced.as_ref().unwrap_or(router), &recording(input, &audio)).await;
    // Whether the provider was reached is read from the outcome: a refusal is zero calls.
    let calls = match &outcome {
        Ok(o) => o.attempts,
        Err(stt::TranscribeError::InvalidAudio(_)) => 0,
        Err(_) => 2,
    };
    Ok(score(input, &outcome, calls))
}

/// A scripted audio endpoint for recorded cases: each call pops the next answer.
struct Recorded {
    steps: std::sync::Mutex<std::collections::VecDeque<Value>>,
    calls: Arc<AtomicU32>,
}
impl Recorded {
    /// A provider whose every transcription fails.
    fn failing() -> Self {
        Self {
            steps: Default::default(),
            calls: Default::default(),
        }
    }
}
#[async_trait]
impl LlmProvider for Recorded {
    fn name(&self) -> &'static str {
        "recorded"
    }
    fn is_fixture(&self) -> bool {
        true
    }
    async fn complete(&self, _: &str, _: &str) -> Result<Completion> {
        bail!("recorded transcription provider has no chat")
    }
    async fn stream(&self, _: &str, _: &str) -> Result<CompletionStream> {
        bail!("recorded transcription provider has no chat")
    }
    async fn transcribe(
        &self,
        route: &Route,
        model: &str,
        _audio: &[u8],
        _container: &str,
        _language: Option<&str>,
    ) -> Result<Transcription> {
        self.calls.fetch_add(1, Ordering::SeqCst);
        // The recorded route must be the private one the engine configures.
        if !route.zdr || route.data_collection.as_deref() != Some("deny") {
            bail!("route is not private");
        }
        let step = self
            .steps
            .lock()
            .unwrap()
            .pop_front()
            .unwrap_or(Value::Null);
        if step.is_null() {
            bail!("recorded provider failure");
        }
        Ok(Transcription {
            text: step["text"].as_str().unwrap_or_default().into(),
            language: step["language"].as_str().unwrap_or_default().into(),
            seconds: step["seconds"].as_f64().unwrap_or_default(),
            cost_nanos: step["cost_nanos"].as_u64().unwrap_or_default(),
            cost_reported: step["cost_nanos"].is_u64(),
            confidence: step["confidence"].as_f64(),
            model: model.into(),
        })
    }
}

/// Offline case: `recorded.primary` and `recorded.fallback` are a transcript object or `null`
/// (provider failure); `audio_bytes` stands in for the recording (never real audio).
pub async fn recorded(input: &Value) -> Result<Scored> {
    let calls = Arc::new(AtomicU32::new(0));
    let provider = |key: &str| -> Arc<dyn LlmProvider> {
        Arc::new(Recorded {
            steps: std::sync::Mutex::new(
                input["recorded"][key]
                    .as_array()
                    .cloned()
                    .unwrap_or_else(|| vec![input["recorded"][key].clone()])
                    .into(),
            ),
            calls: calls.clone(),
        })
    };
    let router = ProviderRouter::new(provider("primary"), provider("fallback"))?;
    let len = input["audio_bytes"].as_u64().unwrap_or(2048) as usize;
    // Constant bytes stand in for quiet audio (`audio_kind: "quiet"`, or no spoken reference);
    // any other case gets a recording with sound, because digital silence never yields text.
    let spoken = !stt::normalized(input["reference"].as_str().unwrap_or_default()).is_empty();
    let audio = if input["audio_kind"] == "speech" || (spoken && input["audio_kind"] != "quiet") {
        let mut x = 0x9E37_79B9_7F4A_7C15u64;
        (0..len)
            .map(|_| {
                x ^= x << 13;
                x ^= x >> 7;
                x ^= x << 17;
                (x >> 24) as u8
            })
            .collect()
    } else {
        vec![1u8; len]
    };
    let outcome = stt::transcribe(&router, &recording(input, &audio)).await;
    Ok(score(input, &outcome, calls.load(Ordering::SeqCst)))
}
