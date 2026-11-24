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
/// Dearest per-second price the fallback has been listed at (1.11e-5 USD per second).
pub const WORST_NANOS_PER_SECOND: u64 = 11_100;
/// Dearest per-second price the primary has been listed at (3.33e-6 USD per second).
pub const PRIMARY_NANOS_PER_SECOND: u64 = 3_330;
/// A primary attempt that times out may still be billed, and the fallback then bills too: a full
/// 30 s recording must fit under `MAX_COST_NANOS` with both at their worst price.
const _: () = assert!(
    MAX_DURATION_MS as u64 / 1000 * (WORST_NANOS_PER_SECOND + PRIMARY_NANOS_PER_SECOND)
        <= MAX_COST_NANOS
);
/// Longest `language_hint` the Engine forwards (`ll`, `lll` or `ll-Xxxx`).
const MAX_LANGUAGE_HINT_BYTES: usize = 8;

/// An HTTP status the provider answered with, carried inside the provider's `anyhow` error so the
/// attempt loop can tell a refused request from an outage.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct HttpStatus(pub u16);
impl std::fmt::Display for HttpStatus {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "transcription provider answered {}", self.0)
    }
}
impl std::error::Error for HttpStatus {}
impl HttpStatus {
    /// A 4xx other than 408 and 429: the same request can never succeed, on any model.
    pub fn is_permanent_rejection(self) -> bool {
        (400..500).contains(&self.0) && !matches!(self.0, 408 | 429)
    }
}

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
    /// False when the provider sent no `usage.cost`; the Engine then estimates it.
    pub cost_reported: bool,
    /// Provider confidence for the whole transcript, 0 to 1, when it scores one.
    pub confidence: Option<f64>,
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
    /// The provider refused the request itself (4xx other than 408/429); retrying cannot help.
    Rejected,
}
impl TranscribeError {
    pub fn reason(&self) -> &'static str {
        match self {
            Self::InvalidAudio(_) => "invalid_audio",
            Self::CostCeiling => "transcribe_cost_ceiling",
            Self::Timeout => "transcribe_timeout",
            Self::Failed => "transcribe_failed",
            Self::Rejected => "transcribe_rejected",
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
    if !valid_language_hint(rec.language_hint) {
        return Err(TranscribeError::InvalidAudio("language_hint"));
    }
    Ok(())
}

/// Empty, or an ISO 639 code with an optional script or region: `^[a-z]{2,3}(-[A-Za-z]{2,4})?$`.
fn valid_language_hint(hint: &str) -> bool {
    if hint.is_empty() {
        return true;
    }
    if hint.len() > MAX_LANGUAGE_HINT_BYTES {
        return false;
    }
    let (language, subtag) = match hint.split_once('-') {
        Some((l, s)) => (l, Some(s)),
        None => (hint, None),
    };
    (2..=3).contains(&language.len())
        && language.bytes().all(|b| b.is_ascii_lowercase())
        && subtag.is_none_or(|s| {
            (2..=4).contains(&s.len()) && s.bytes().all(|b| b.is_ascii_alphabetic())
        })
}

/// Words Whisper-class models invent on silence or room noise. Only the fallback is filtered, only
/// when the whole transcript is one of these, and only when the audio itself is quiet (see
/// `audio_is_quiet`), so a real sentence, or a real spoken "thank you", is never touched.
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

/// Provider confidence below this, when reported, marks the audio as not holding speech.
const QUIET_CONFIDENCE: f64 = 0.5;
/// Shortest recording the repetition test is applied to.
const MIN_QUIET_PROBE_BYTES: usize = 512;
/// Digital silence encodes to the same few packets over and over: fewer than 60% of its 8-byte
/// windows are distinct (0.29 for a Opus clip, 0.44 for AAC), against 0.98 or more for any
/// recording with sound in it (speech, room noise, music).
const QUIET_DISTINCT_WINDOWS: f64 = 0.6;

/// Whether the recording carries no sound: the provider scored it with low confidence, or the
/// encoded bytes are the repeating packets of digital silence. The OpenRouter transcription
/// response exposes no `no_speech_prob`, so absent either signal the audio counts as speech.
fn audio_is_quiet(audio: &[u8], confidence: Option<f64>) -> bool {
    if confidence.is_some_and(|c| c.is_finite() && c < QUIET_CONFIDENCE) {
        return true;
    }
    is_digital_silence(audio)
}

/// Whether the encoded bytes are the repeating packets of digital silence (no provider signal).
fn is_digital_silence(audio: &[u8]) -> bool {
    if audio.len() < MIN_QUIET_PROBE_BYTES {
        return false;
    }
    let windows = audio.len() - 7;
    let distinct: std::collections::HashSet<u64> = audio
        .windows(8)
        .map(|w| u64::from_le_bytes(w.try_into().expect("window of 8")))
        .collect();
    (distinct.len() as f64) / (windows as f64) < QUIET_DISTINCT_WINDOWS
}

/// Whether `text` is dropped as invented: punctuation only, or a blocklisted phrase on quiet audio.
fn is_silence_hallucination(text: &str, audio: &[u8], confidence: Option<f64>) -> bool {
    let n = normalized(text);
    n.is_empty()
        || (SILENCE_HALLUCINATIONS.contains(&n.as_str()) && audio_is_quiet(audio, confidence))
}

/// Worst price of `seconds` of audio at `nanos_per_second`, rounded up to whole seconds.
fn worst_nanos(seconds: f64, nanos_per_second: u64) -> u64 {
    let s = if seconds.is_finite() {
        seconds.max(0.0)
    } else {
        0.0
    };
    (s.ceil() as u64).saturating_mul(nanos_per_second)
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
    // Worst-case spend on attempts that were dropped client-side but may be billed upstream.
    let mut lost_nanos = 0u64;
    let claimed_seconds = f64::from(rec.duration_ms) / 1000.0;
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
                if !t.cost_reported {
                    let rate = if fallback {
                        WORST_NANOS_PER_SECOND
                    } else {
                        PRIMARY_NANOS_PER_SECOND
                    };
                    t.cost_nanos = worst_nanos(t.seconds.max(claimed_seconds), rate);
                }
                t.cost_nanos = t.cost_nanos.saturating_add(lost_nanos);
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
                // Digital silence holds no speech, so whatever either route heard is invented.
                if is_digital_silence(rec.audio)
                    || (fallback && is_silence_hallucination(&t.text, rec.audio, t.confidence))
                {
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
            Ok(Err(e)) => {
                let status = e.downcast_ref::<HttpStatus>().copied();
                tracing::warn!(
                    event = "transcribe_attempt_failed",
                    model = %route.model,
                    fallback,
                    error_class = match status {
                        Some(HttpStatus(c)) if c >= 500 => "http_5xx",
                        Some(HttpStatus(c)) if c >= 400 => "http_4xx",
                        Some(_) => "http_other",
                        None => "transport_or_response",
                    },
                    status = status.map(|s| s.0),
                    "transcription attempt failed"
                );
                if status.is_some_and(HttpStatus::is_permanent_rejection) {
                    return Err(TranscribeError::Rejected);
                }
            }
            Err(_) => {
                timed_out += 1;
                lost_nanos = lost_nanos.saturating_add(worst_nanos(
                    claimed_seconds,
                    if fallback {
                        WORST_NANOS_PER_SECOND
                    } else {
                        PRIMARY_NANOS_PER_SECOND
                    },
                ));
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
