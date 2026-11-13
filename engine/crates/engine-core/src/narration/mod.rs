//! Per-slide speech synthesis. Only a deterministic mock exists in E14-01; the
//! ElevenLabs HTTP client arrives in E14-04 behind the same `SpeechProvider` trait.
use async_trait::async_trait;
use std::time::Duration;

/// Mock voices bill as the vendor so quotes and settlement validate identically.
pub const BILLING_PROVIDER: &str = "elevenlabs";
const MOCK_MS_PER_CHAR: u32 = 50;
const SAMPLE_RATE: u32 = 8000;
/// Alignment may end this far from the audio duration.
const DURATION_TOLERANCE_MS: u32 = 250;

#[derive(Debug, Clone, PartialEq)]
pub struct SpeechRequest {
    pub text: String,
    pub language: String,
    pub voice_id: String,
    pub model_id: String,
}

/// One entry per Unicode scalar value of the text, in order.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct Alignment {
    pub characters: Vec<String>,
    pub start_ms: Vec<u32>,
    pub end_ms: Vec<u32>,
}

#[derive(Debug, Clone, PartialEq)]
pub struct SpeechOutput {
    pub audio: Vec<u8>,
    pub format: String,
    pub duration_ms: u32,
    pub alignment: Alignment,
}

#[derive(Debug)]
pub enum SpeechError {
    MissingText,
    InvalidAlignment(String),
    Provider(String),
    Timeout,
}

impl std::fmt::Display for SpeechError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::MissingText => f.write_str("narration text missing"),
            Self::InvalidAlignment(m) => write!(f, "invalid alignment: {m}"),
            Self::Provider(m) => write!(f, "speech provider failed: {m}"),
            Self::Timeout => f.write_str("speech call timed out"),
        }
    }
}
impl std::error::Error for SpeechError {}

#[async_trait]
pub trait SpeechProvider: Send + Sync {
    fn name(&self) -> &'static str;
    async fn synthesize(&self, req: &SpeechRequest) -> anyhow::Result<SpeechOutput>;
}

/// Voice settings come from engine config, never from the caller.
#[derive(Debug, Clone, PartialEq)]
pub struct VoiceConfig {
    pub voice_id: String,
    pub model_id: String,
    pub stability: f32,
    pub similarity_boost: f32,
    pub speed: f32,
}

impl VoiceConfig {
    /// One configured default voice for now (ADR 0012); live voices stay disabled.
    pub fn resolve(key: &str) -> Option<Self> {
        (key == "default").then(|| Self {
            voice_id: "mock-default-voice".into(),
            model_id: "eleven_flash_v2_5".into(),
            stability: 0.5,
            similarity_boost: 0.75,
            speed: 1.0,
        })
    }

    /// Stable across processes (FNV-1a), unlike `DefaultHasher`.
    pub fn fingerprint(&self) -> String {
        let canonical = format!(
            "v1|{}|{:.3}|{:.3}|{:.3}",
            self.voice_id, self.stability, self.similarity_boost, self.speed
        );
        format!("fp1-{:016x}", fnv1a(canonical.as_bytes()))
    }
}

fn fnv1a(bytes: &[u8]) -> u64 {
    bytes.iter().fold(0xcbf29ce484222325, |h, b| {
        (h ^ u64::from(*b)).wrapping_mul(0x100000001b3)
    })
}

/// Deterministic stand-in: a quiet tone whose length follows the text.
pub struct MockSpeechProvider;

#[async_trait]
impl SpeechProvider for MockSpeechProvider {
    fn name(&self) -> &'static str {
        "mock"
    }
    async fn synthesize(&self, req: &SpeechRequest) -> anyhow::Result<SpeechOutput> {
        let chars: Vec<String> = req.text.chars().map(String::from).collect();
        let n = chars.len() as u32;
        let duration_ms = n * MOCK_MS_PER_CHAR;
        let samples = (SAMPLE_RATE / 1000) * duration_ms;
        let mut audio = Vec::with_capacity(44 + samples as usize);
        audio.extend_from_slice(b"RIFF");
        audio.extend_from_slice(&(36 + samples).to_le_bytes());
        audio.extend_from_slice(b"WAVEfmt ");
        audio.extend_from_slice(&16u32.to_le_bytes());
        audio.extend_from_slice(&1u16.to_le_bytes()); // PCM
        audio.extend_from_slice(&1u16.to_le_bytes()); // mono
        audio.extend_from_slice(&SAMPLE_RATE.to_le_bytes());
        audio.extend_from_slice(&SAMPLE_RATE.to_le_bytes()); // byte rate, 8-bit
        audio.extend_from_slice(&1u16.to_le_bytes());
        audio.extend_from_slice(&8u16.to_le_bytes());
        audio.extend_from_slice(b"data");
        audio.extend_from_slice(&samples.to_le_bytes());
        // 200 Hz square wave at low amplitude around the 8-bit midpoint.
        audio.extend((0..samples).map(|i| if (i / 20) % 2 == 0 { 136u8 } else { 120u8 }));
        Ok(SpeechOutput {
            audio,
            format: "audio/wav".into(),
            duration_ms,
            alignment: Alignment {
                characters: chars,
                start_ms: (0..n).map(|i| i * MOCK_MS_PER_CHAR).collect(),
                end_ms: (1..=n).map(|i| i * MOCK_MS_PER_CHAR).collect(),
            },
        })
    }
}

/// Rejects output the player could not honour: missing text, gaps, reversals or drift.
pub fn validate(text: &str, out: &SpeechOutput) -> Result<(), SpeechError> {
    if text.trim().is_empty() {
        return Err(SpeechError::MissingText);
    }
    let bad = |m: &str| Err(SpeechError::InvalidAlignment(m.into()));
    let a = &out.alignment;
    let expected: Vec<String> = text.chars().map(String::from).collect();
    if out.audio.is_empty() || out.duration_ms == 0 {
        return bad("audio missing");
    }
    if a.characters != expected
        || a.start_ms.len() != expected.len()
        || a.end_ms.len() != expected.len()
    {
        return bad("alignment must cover every character");
    }
    let mut previous = 0;
    for (start, end) in a.start_ms.iter().zip(&a.end_ms) {
        if end < start || *start < previous {
            return bad("timestamps must be non-decreasing");
        }
        previous = *start;
    }
    let last = a.end_ms.last().copied().unwrap_or(0);
    if last.abs_diff(out.duration_ms) > DURATION_TOLERANCE_MS {
        return bad("alignment must end near the audio duration");
    }
    Ok(())
}

/// Per-call timeout, bounded attempts, and output validation on every attempt.
/// Returns the output and the attempt number that produced it.
pub async fn synthesize_bounded(
    provider: &dyn SpeechProvider,
    req: &SpeechRequest,
    timeout: Duration,
    max_attempts: u32,
) -> Result<(SpeechOutput, u32), SpeechError> {
    let mut last = SpeechError::Provider("no attempt".into());
    for attempt in 1..=max_attempts.max(1) {
        last = match tokio::time::timeout(timeout, provider.synthesize(req)).await {
            Err(_) => SpeechError::Timeout,
            Ok(Err(e)) => SpeechError::Provider(e.to_string()),
            Ok(Ok(out)) => match validate(&req.text, &out) {
                Ok(()) => return Ok((out, attempt)),
                Err(e @ SpeechError::MissingText) => return Err(e),
                Err(e) => e,
            },
        };
    }
    Err(last)
}

/// Actual usage of one delivered audio attempt.
#[derive(Debug, Clone, PartialEq)]
pub struct SpeechUsage {
    pub provider: String,
    pub model: String,
    pub component: String,
    pub narration_characters: u32,
    pub attempt: u32,
    pub latency_ms: u32,
    pub delivered: bool,
}

pub fn usage_for(model: &str, characters: u32, attempt: u32, latency_ms: u32) -> SpeechUsage {
    SpeechUsage {
        provider: BILLING_PROVIDER.into(),
        model: model.into(),
        component: "audio".into(),
        narration_characters: characters,
        attempt,
        latency_ms,
        delivered: true,
    }
}

#[cfg(test)]
mod tests;
