//! Voice transcription (`Transcribe` RPC, task `transcribe`).
//!
//! One short recording goes to the OpenRouter audio transcription endpoint on the single AI route:
//! the primary model, then the fallback once, each with its own timeout, data collection denied and
//! zero data retention required. The audio is a borrowed byte slice for the length of the call. It is
//! never written, logged, hashed or kept, and neither is the transcript: only sizes, model ids,
//! latency and cost leave this module.
use crate::{LlmProvider, ProviderRouter};
use std::time::Duration;
use tokio::time::Instant;

/// Longest recording the API admits (spec: voice-input).
pub const MAX_DURATION_MS: u32 = 30_000;
/// Shortest recording worth transcribing.
pub const MIN_DURATION_MS: u32 = 500;
/// Largest upload the API admits, and the Engine refuses anything bigger itself.
pub const MAX_AUDIO_BYTES: usize = 1_000_000;
/// Hard timeout per model attempt, so primary plus one fallback finish within 16 s.
pub const ATTEMPT_TIMEOUT: Duration = Duration::from_secs(8);
/// No request may cost more than this (0.0005 USD, in nanodollars).
pub const MAX_COST_NANOS: u64 = 500_000;
/// Dearest per-second price either model has been listed at (1.11e-5 USD per second). A full
/// 30 s recording must still fit under `MAX_COST_NANOS` at that price.
pub const WORST_NANOS_PER_SECOND: u64 = 11_100;
const _: () = assert!(MAX_DURATION_MS as u64 / 1000 * WORST_NANOS_PER_SECOND <= MAX_COST_NANOS);

/// What a provider returns for one recording.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct Transcription {
    pub text: String,
    /// ISO 639-1 when the model reports one.
    pub language: String,
    /// Audio seconds the provider billed (0.0 when unreported).
    pub seconds: f64,
    /// Provider-reported USD cost in nanodollars (0 when unreported).
    pub cost_nanos: u64,
    pub model: String,
}

/// One recording to transcribe. `container` is `webm` or `m4a`.
#[derive(Debug, Clone, Copy)]
pub struct Recording<'a> {
    pub audio: &'a [u8],
    pub container: &'a str,
    pub duration_ms: u32,
    pub language_hint: &'a str,
}

/// Typed failures; `reason` is the gRPC `ErrorInfo` reason.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum TranscribeError {
    /// The request itself is not transcribable (empty, too large, too long, unknown container).
    InvalidAudio(&'static str),
    /// A provider answered above the per-request cost ceiling; nothing is delivered or retried.
    CostCeiling,
    /// Every attempt timed out.
    Timeout,
    /// Every attempt failed.
    Failed,
}
impl TranscribeError {
    pub fn reason(&self) -> &'static str {
        match self {
            Self::InvalidAudio(_) => "invalid_audio",
            Self::CostCeiling => "transcribe_cost_ceiling",
            Self::Timeout => "transcribe_timeout",
            Self::Failed => "transcribe_failed",
        }
    }
}

/// A delivered transcript and how it was served.
#[derive(Debug, Clone, PartialEq)]
pub struct Outcome {
    pub transcription: Transcription,
    pub fallback_used: bool,
    pub latency_ms: u32,
    /// Provider attempts made, 1 or 2.
    pub attempts: u32,
}

/// Rejects what no provider should see. Runs before any provider call.
pub fn validate(rec: &Recording<'_>) -> Result<(), TranscribeError> {
    if rec.audio.is_empty() {
        return Err(TranscribeError::InvalidAudio("empty"));
    }
    if rec.audio.len() > MAX_AUDIO_BYTES {
        return Err(TranscribeError::InvalidAudio("too_large"));
    }
    if !(MIN_DURATION_MS..=MAX_DURATION_MS).contains(&rec.duration_ms) {
        return Err(TranscribeError::InvalidAudio("duration"));
    }
    if !matches!(rec.container, "webm" | "m4a") {
        return Err(TranscribeError::InvalidAudio("container"));
    }
    Ok(())
}

/// Words Whisper-class models invent on silence or room noise. Only the fallback is filtered, and
/// only when the whole transcript is one of these, so a real sentence is never touched.
const SILENCE_HALLUCINATIONS: [&str; 6] = [
    "thank you",
    "thanks for watching",
    "thanks",
    "you",
    "bye",
    "okay",
];

/// The Golden set's text rule: lowercase, hyphens become spaces, apostrophes vanish, punctuation
/// and symbols are dropped (combining marks stay), runs of spaces collapse. Vietnamese is scored on
/// the same space-separated syllables.
pub fn normalized(text: &str) -> String {
    let mut out = String::with_capacity(text.len());
    for c in text.chars().flat_map(char::to_lowercase) {
        match c {
            '-' => out.push(' '),
            '\'' | '\u{2019}' => {}
            c if c.is_whitespace() => out.push(' '),
            c if c.is_alphanumeric() || ('\u{0300}'..='\u{036f}').contains(&c) => out.push(c),
            _ => {}
        }
    }
    out.split_whitespace().collect::<Vec<_>>().join(" ")
}

/// Word error rate of `hypothesis` against `reference` (word-level Levenshtein distance divided
/// by reference words). `None` when the reference has no words (silence cases).
pub fn wer(reference: &str, hypothesis: &str) -> Option<f64> {
    let (r, h) = (normalized(reference), normalized(hypothesis));
    let r: Vec<&str> = r.split(' ').filter(|w| !w.is_empty()).collect();
    let h: Vec<&str> = h.split(' ').filter(|w| !w.is_empty()).collect();
    if r.is_empty() {
        return None;
    }
    let mut row: Vec<usize> = (0..=h.len()).collect();
    for i in 1..=r.len() {
        let mut prev = row[0];
        row[0] = i;
        for j in 1..=h.len() {
            let cur = row[j];
            row[j] = (row[j] + 1)
                .min(row[j - 1] + 1)
                .min(prev + usize::from(r[i - 1] != h[j - 1]));
            prev = cur;
        }
    }
    Some(row[h.len()] as f64 / r.len() as f64)
}

fn is_silence_hallucination(text: &str) -> bool {
    let n = normalized(text);
    n.is_empty() || SILENCE_HALLUCINATIONS.contains(&n.as_str())
}

/// Transcribes through the primary model, then the fallback once.
///
/// A provider failure or timeout moves to the fallback; a cost above the ceiling stops the chain
/// (the audio is not sent anywhere else). An empty transcript is a valid answer, not a failure.
pub async fn transcribe(
    router: &ProviderRouter,
    rec: &Recording<'_>,
) -> Result<Outcome, TranscribeError> {
    validate(rec)?;
    let model = router
        .models
        .tasks
        .get("transcribe")
        .ok_or(TranscribeError::Failed)?;
    let started = Instant::now();
    let hint = (!rec.language_hint.is_empty()).then_some(rec.language_hint);
    let mut timed_out = 0;
    for (index, (provider, fallback)) in [(&router.primary, false), (&router.fallback, true)]
        .into_iter()
        .enumerate()
    {
        let route = model.attempt_route(fallback);
        let attempt = tokio::time::timeout(
            ATTEMPT_TIMEOUT,
            LlmProvider::transcribe(
                provider.as_ref(),
                &route,
                &route.model,
                rec.audio,
                rec.container,
                hint,
            ),
        )
        .await;
        match attempt {
            Ok(Ok(mut t)) => {
                if t.cost_nanos > MAX_COST_NANOS {
                    tracing::error!(
                        event = "transcribe_cost_ceiling",
                        model = %route.model,
                        cost_nanos = t.cost_nanos,
                        "transcription above the per-request cost ceiling"
                    );
                    return Err(TranscribeError::CostCeiling);
                }
                t.text = t.text.trim().to_string();
                if fallback && is_silence_hallucination(&t.text) {
                    t.text.clear();
                }
                if t.model.is_empty() {
                    t.model = route.model.clone();
                }
                return Ok(Outcome {
                    transcription: t,
                    fallback_used: fallback,
                    latency_ms: started.elapsed().as_millis().min(u32::MAX as u128) as u32,
                    attempts: index as u32 + 1,
                });
            }
            Ok(Err(_)) => tracing::warn!(
                event = "transcribe_attempt_failed",
                model = %route.model,
                fallback,
                "transcription attempt failed"
            ),
            Err(_) => {
                timed_out += 1;
                tracing::warn!(
                    event = "transcribe_attempt_timeout",
                    model = %route.model,
                    fallback,
                    "transcription attempt timed out"
                );
            }
        }
    }
    Err(if timed_out == 2 {
        TranscribeError::Timeout
    } else {
        TranscribeError::Failed
    })
}

#[cfg(test)]
mod tests;
