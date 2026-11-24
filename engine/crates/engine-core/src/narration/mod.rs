//! Per-Slide speech synthesis and character alignment.
use async_trait::async_trait;
use std::time::Duration;
use unicode_segmentation::UnicodeSegmentation;

/// Mock voices bill as the vendor so quotes and settlement validate identically.
pub const BILLING_PROVIDER: &str = "elevenlabs";
const MOCK_MS_PER_CHAR: u32 = 50;
const SAMPLE_RATE: u32 = 8000;
/// Alignment may end this far from the audio duration.
const DURATION_TOLERANCE_MS: u32 = 250;
/// Launch limits (E14-04): one TTS call may take 60 s; one repair/retry follows the primary.
pub const MAX_TTS_TIMEOUT_MS: u32 = 60_000;
pub const MAX_TTS_ATTEMPTS: u32 = 2;
/// Keep the binary response within the player's bounded object reads.
pub const MAX_AUDIO_BYTES: usize = 1 << 20;
/// Pause before the single retry so a rate-limited vendor is not hit again at once.
const RETRY_BACKOFF: Duration = Duration::from_millis(250);

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
    Provider(ProviderFailure),
    /// The vendor kept answering 429 through the allowed retry.
    RateLimited,
    Timeout,
}

#[derive(Debug)]
pub enum ProviderFailure {
    Rejected,
    Unavailable(String),
}

/// Classified vendor failure. Messages never carry keys, bodies or narration text.
#[derive(Debug, Clone, PartialEq)]
pub enum VendorFailure {
    /// Retrying cannot help (bad key, rejected request).
    Permanent(String),
    RateLimited,
    Unavailable(String),
}

impl std::fmt::Display for VendorFailure {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Permanent(m) => write!(f, "vendor rejected the request: {m}"),
            Self::RateLimited => f.write_str("vendor rate limited"),
            Self::Unavailable(m) => write!(f, "vendor unavailable: {m}"),
        }
    }
}
impl std::error::Error for VendorFailure {}

impl std::fmt::Display for SpeechError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::MissingText => f.write_str("narration text missing"),
            Self::InvalidAlignment(m) => write!(f, "invalid alignment: {m}"),
            Self::Provider(ProviderFailure::Unavailable(m)) => {
                write!(f, "speech provider failed: {m}")
            }
            Self::Provider(ProviderFailure::Rejected) => {
                f.write_str("speech provider permanently rejected the request")
            }
            Self::RateLimited => f.write_str("speech provider rate limited"),
            Self::Timeout => f.write_str("speech call timed out"),
        }
    }
}
impl std::error::Error for SpeechError {}

#[async_trait]
pub trait SpeechProvider: Send + Sync {
    fn name(&self) -> &'static str;
    async fn synthesize(&self, req: &SpeechRequest) -> anyhow::Result<SpeechOutput>;
    /// Voice, model and settings for a logical voice key; configuration, never the caller.
    fn voice(&self, key: &str) -> Option<VoiceConfig> {
        VoiceConfig::resolve(key)
    }
    fn routed_voice(&self, key: &str, _language: &str) -> Option<VoiceConfig> {
        self.voice(key)
    }
    fn alternate(&self, _language: &str, _model: &str) -> Option<&dyn SpeechProvider> {
        None
    }
    fn alternate_models(&self, _language: &str) -> Vec<&'static str> {
        Vec::new()
    }
    fn audio_format(&self) -> &'static str {
        "audio/wav"
    }
    fn timing_source(&self) -> &'static str {
        "vendor"
    }
}

/// Recover character intervals from audio without splitting visible graphemes. Whitespace
/// consumes no duration and every Unicode scalar still has one entry in the wire contract.
pub fn estimated_alignment(text: &str, duration_ms: u32) -> Result<Alignment, SpeechError> {
    let weight = |g: &str| u64::from(!g.chars().all(char::is_whitespace));
    let total: u64 = text.graphemes(true).map(weight).sum();
    if total == 0 {
        return Err(SpeechError::MissingText);
    }
    if !(1..=MAX_TTS_TIMEOUT_MS).contains(&duration_ms) {
        return Err(SpeechError::InvalidAlignment(
            "audio duration outside bounds".into(),
        ));
    }
    let mut alignment = Alignment::default();
    let (mut cumulative, mut start) = (0, 0);
    for grapheme in text.graphemes(true) {
        cumulative += weight(grapheme);
        let end = (u64::from(duration_ms) * cumulative / total) as u32;
        for scalar in grapheme.chars() {
            alignment.characters.push(scalar.to_string());
            alignment.start_ms.push(start);
            alignment.end_ms.push(end);
        }
        start = end;
    }
    Ok(alignment)
}

/// Count decoded samples, including variable-bitrate MP3 and encoder padding handled by
/// the demuxer. A malformed packet or a format change invalidates the whole asset.
pub fn decoded_mp3_duration_ms(audio: &[u8]) -> Option<u32> {
    let decoded = decode_mp3(audio, false)?;
    Some(decoded.duration_ms())
}

struct DecodedMp3 {
    frames: u64,
    rate: u32,
    channels: u16,
    samples: Vec<i16>,
}

impl DecodedMp3 {
    fn duration_ms(&self) -> u32 {
        (self.frames * 1000 / u64::from(self.rate)) as u32
    }
}

fn decode_mp3(audio: &[u8], collect_pcm: bool) -> Option<DecodedMp3> {
    use symphonia::core::{
        formats::{TrackType, probe::Hint},
        io::MediaSourceStream,
    };

    if audio.is_empty() || audio.len() > MAX_AUDIO_BYTES {
        return None;
    }
    let input = MediaSourceStream::new(
        Box::new(std::io::Cursor::new(audio.to_vec())),
        Default::default(),
    );
    let mut hint = Hint::new();
    hint.with_extension("mp3");
    let mut format = symphonia::default::get_probe()
        .probe(&hint, input, Default::default(), Default::default())
        .ok()?;
    let track = format.default_track(TrackType::Audio)?;
    let track_id = track.id;
    let params = track.codec_params.as_ref()?.audio()?;
    let mut decoder = symphonia::default::get_codecs()
        .make_audio_decoder(params, &Default::default())
        .ok()?;
    let mut frames = 0_u64;
    let mut format_spec = None;
    let mut samples = Vec::new();
    while let Some(packet) = format.next_packet().ok()? {
        if packet.track_id != track_id {
            return None;
        }
        let decoded = decoder.decode(&packet).ok()?;
        let spec = (decoded.spec().rate(), decoded.spec().channels().count());
        if spec.0 == 0 || spec.1 == 0 || *format_spec.get_or_insert(spec) != spec {
            return None;
        }
        frames += decoded.frames() as u64;
        if frames * 1000 > u64::from(spec.0) * u64::from(MAX_TTS_TIMEOUT_MS) {
            return None;
        }
        if collect_pcm {
            if (samples.len() + decoded.samples_interleaved()) * 2 + 44 > MAX_AUDIO_BYTES {
                return None;
            }
            decoded.copy_to_vec_interleaved(&mut samples);
        }
    }
    let (rate, channels) = format_spec?;
    let result = DecodedMp3 {
        frames,
        rate,
        channels: u16::try_from(channels).ok()?,
        samples,
    };
    (result.duration_ms() > 0).then_some(result)
}

/// Split an utterance without losing whitespace or splitting a Unicode grapheme.
/// The UTF-8 byte limit is a conservative guard for textual provider input limits.
pub fn speech_chunks(mut text: &str, limit: usize) -> Option<Vec<&str>> {
    if text.trim().is_empty() {
        return None;
    }
    let mut chunks = Vec::new();
    while text.len() > limit {
        let mut last = 0;
        let mut boundary = 0;
        for (start, grapheme) in text.grapheme_indices(true) {
            let end = start + grapheme.len();
            if end > limit {
                break;
            }
            last = end;
            if grapheme.chars().all(char::is_whitespace) && !text[..start].trim().is_empty() {
                boundary = end;
            }
        }
        let split = if boundary > 0 { boundary } else { last };
        if split == 0 || text[..split].trim().is_empty() {
            return None;
        }
        chunks.push(&text[..split]);
        text = &text[split..];
    }
    if text.trim().is_empty() {
        return None;
    }
    chunks.push(text);
    Some(chunks)
}

/// Decode each fragment independently and write one playable PCM WAV for a split
/// utterance. Joining MP3 files directly would retain multiple file-level headers.
pub fn finish_speech_mp3s(mut parts: Vec<(&str, Vec<u8>)>) -> Result<SpeechOutput, SpeechError> {
    let invalid = || SpeechError::InvalidAlignment("invalid decoded audio".into());
    if parts.len() == 1 {
        let (text, audio) = parts.pop().ok_or_else(invalid)?;
        let duration_ms = decoded_mp3_duration_ms(&audio).ok_or_else(invalid)?;
        return Ok(SpeechOutput {
            audio,
            format: "audio/mpeg".into(),
            duration_ms,
            alignment: estimated_alignment(text, duration_ms)?,
        });
    }
    let mut alignment = Alignment::default();
    let mut pcm = Vec::new();
    let mut spec = None;
    let (mut frames, mut duration_ms) = (0, 0);
    for (text, audio) in parts {
        let decoded = decode_mp3(&audio, true).ok_or_else(invalid)?;
        let this = (decoded.rate, decoded.channels);
        if *spec.get_or_insert(this) != this
            || (pcm.len() + decoded.samples.len()) * 2 + 44 > MAX_AUDIO_BYTES
        {
            return Err(invalid());
        }
        pcm.extend(decoded.samples);
        frames += decoded.frames;
        let end = (frames * 1000 / u64::from(decoded.rate)) as u32;
        if end > MAX_TTS_TIMEOUT_MS {
            return Err(invalid());
        }
        let part = estimated_alignment(text, end - duration_ms)?;
        alignment.characters.extend(part.characters);
        alignment
            .start_ms
            .extend(part.start_ms.into_iter().map(|ms| ms + duration_ms));
        alignment
            .end_ms
            .extend(part.end_ms.into_iter().map(|ms| ms + duration_ms));
        duration_ms = end;
    }
    let (rate, channels) = spec.ok_or_else(invalid)?;
    let block = channels * 2;
    let size = (pcm.len() * 2) as u32;
    let mut audio = Vec::with_capacity(44 + size as usize);
    audio.extend_from_slice(b"RIFF");
    audio.extend_from_slice(&(36 + size).to_le_bytes());
    audio.extend_from_slice(b"WAVEfmt ");
    audio.extend_from_slice(&16_u32.to_le_bytes());
    audio.extend_from_slice(&1_u16.to_le_bytes()); // PCM
    audio.extend_from_slice(&channels.to_le_bytes());
    audio.extend_from_slice(&rate.to_le_bytes());
    audio.extend_from_slice(&(rate * u32::from(block)).to_le_bytes());
    audio.extend_from_slice(&block.to_le_bytes());
    audio.extend_from_slice(&16_u16.to_le_bytes());
    audio.extend_from_slice(b"data");
    audio.extend_from_slice(&size.to_le_bytes());
    audio.extend(pcm.into_iter().flat_map(i16::to_le_bytes));
    Ok(SpeechOutput {
        audio,
        format: "audio/wav".into(),
        duration_ms,
        alignment,
    })
}

/// Duration of a delivered audio file, so alignment is checked against the audio itself.
/// WAV is read from its header; CBR MP3 is estimated from the format's bitrate.
pub fn audio_duration_ms(output_format: &str, audio: &[u8]) -> Option<u32> {
    if output_format.starts_with("wav_") {
        let le32 = |at: usize| Some(u32::from_le_bytes(audio.get(at..at + 4)?.try_into().ok()?));
        if audio.get(..4)? != b"RIFF" || audio.get(8..12)? != b"WAVE" {
            return None;
        }
        let (mut at, mut byte_rate, mut data) = (12, 0, None);
        while at + 8 <= audio.len() {
            let size = le32(at + 4)? as usize;
            match audio.get(at..at + 4)? {
                b"fmt " => byte_rate = le32(at + 16)?,
                b"data" => {
                    // Streamed WAV may declare an unknown size; use what arrived.
                    data = Some(size.min(audio.len() - at - 8));
                    break;
                }
                _ => {}
            }
            at += 8 + size + (size & 1);
        }
        let ms = u64::try_from(data?).ok()? * 1000 / u64::from(byte_rate.max(1));
        return (byte_rate > 0).then(|| u32::try_from(ms).unwrap_or(u32::MAX));
    }
    let kbps: u64 = output_format
        .strip_prefix("mp3_")?
        .rsplit('_')
        .next()?
        .parse()
        .ok()?;
    let ms = audio.len() as u64 * 8 / kbps.max(1);
    u32::try_from(ms).ok()
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

/// The approved voice set: the configured default voice plus test fixtures (ADR 0012, E14-05/06).
/// Vendor voice ids and settings live in `voices.json`, never in code; live voices stay disabled.
pub(crate) const VOICES_JSON: &str = include_str!("voices.json");

#[derive(serde::Deserialize)]
struct VoiceEntry {
    key: String,
    voice_id: String,
    model_id: String,
    stability: f32,
    similarity_boost: f32,
    speed: f32,
}

#[derive(serde::Deserialize)]
struct VoiceFile {
    voices: Vec<VoiceEntry>,
}

impl VoiceConfig {
    /// Resolves a logical voice key from the embedded config.
    pub fn resolve(key: &str) -> Option<Self> {
        let file: VoiceFile = serde_json::from_str(VOICES_JSON).ok()?;
        file.voices
            .into_iter()
            .find(|v| v.key == key)
            .map(|v| Self {
                voice_id: v.voice_id,
                model_id: v.model_id,
                stability: v.stability,
                similarity_boost: v.similarity_boost,
                speed: v.speed,
            })
    }

    /// Every logical key in the config, in order.
    pub fn keys() -> Vec<String> {
        serde_json::from_str::<VoiceFile>(VOICES_JSON)
            .map(|f| f.voices.into_iter().map(|v| v.key).collect())
            .unwrap_or_default()
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
    let mut last = SpeechError::Provider(ProviderFailure::Unavailable("no attempt".into()));
    for attempt in 1..=max_attempts.max(1) {
        if attempt > 1 {
            tokio::time::sleep(RETRY_BACKOFF).await;
        }
        last = match tokio::time::timeout(timeout, provider.synthesize(req)).await {
            Err(_) => SpeechError::Timeout,
            Ok(Err(e)) => match e.downcast_ref::<VendorFailure>() {
                Some(VendorFailure::Permanent(_)) => {
                    return Err(SpeechError::Provider(ProviderFailure::Rejected));
                }
                Some(VendorFailure::RateLimited) => SpeechError::RateLimited,
                _ => SpeechError::Provider(ProviderFailure::Unavailable(e.to_string())),
            },
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
