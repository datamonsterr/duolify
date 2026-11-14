//! ElevenLabs text-to-speech with character timestamps (ADR 0009, E14-04).
//!
//! `POST {base}/v1/text-to-speech/{voice_id}/with-timestamps?output_format=...` with the
//! `xi-api-key` header returns `{audio_base64, alignment{characters,
//! character_start_times_seconds, character_end_times_seconds}}`.
use anyhow::{Result, anyhow};
use async_trait::async_trait;
use base64::{Engine as _, engine::general_purpose::STANDARD};
use engine_core::narration::{
    Alignment, MAX_TTS_TIMEOUT_MS, MockSpeechProvider, SpeechOutput, SpeechProvider, SpeechRequest,
    VendorFailure, VoiceConfig, audio_duration_ms,
};
use serde::Deserialize;
use std::{fmt, sync::Arc, time::Duration};

const DEFAULT_BASE_URL: &str = "https://api.elevenlabs.io";
const DEFAULT_MODEL: &str = "eleven_flash_v2_5";
/// WAV keeps duration exact and matches the stored `audio.wav` object.
const DEFAULT_OUTPUT_FORMAT: &str = "wav_16000";

#[derive(Clone)]
pub struct ElevenLabsConfig {
    pub api_key: String,
    pub base_url: String,
    pub voice: VoiceConfig,
    pub output_format: String,
}

pub struct ElevenLabsSpeech {
    client: reqwest::Client,
    cfg: ElevenLabsConfig,
}

impl fmt::Debug for ElevenLabsSpeech {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("ElevenLabsSpeech")
            .field("base_url", &self.cfg.base_url)
            .field("voice_id", &self.cfg.voice.voice_id)
            .field("api_key", &"<redacted>")
            .finish()
    }
}

#[derive(Deserialize)]
struct Reply {
    audio_base64: Option<String>,
    alignment: Option<RawAlignment>,
}
#[derive(Deserialize)]
struct RawAlignment {
    characters: Vec<String>,
    character_start_times_seconds: Vec<f64>,
    character_end_times_seconds: Vec<f64>,
}

fn millis(seconds: f64) -> Result<u32> {
    if !seconds.is_finite() || !(0.0..=4_000_000.0).contains(&seconds) {
        return Err(VendorFailure::Unavailable("malformed timestamp".into()).into());
    }
    Ok((seconds * 1000.0).round() as u32)
}

impl ElevenLabsSpeech {
    pub fn new(cfg: ElevenLabsConfig) -> Self {
        let client = reqwest::Client::builder()
            .timeout(Duration::from_millis(u64::from(MAX_TTS_TIMEOUT_MS)))
            .build()
            .expect("http client");
        Self { client, cfg }
    }
}

#[async_trait]
impl SpeechProvider for ElevenLabsSpeech {
    fn name(&self) -> &'static str {
        "elevenlabs"
    }

    fn voice(&self, key: &str) -> Option<VoiceConfig> {
        (key == "default").then(|| self.cfg.voice.clone())
    }

    async fn synthesize(&self, req: &SpeechRequest) -> Result<SpeechOutput> {
        let url = format!(
            "{}/v1/text-to-speech/{}/with-timestamps?output_format={}",
            self.cfg.base_url.trim_end_matches('/'),
            req.voice_id,
            self.cfg.output_format
        );
        let v = &self.cfg.voice;
        let mut body = serde_json::json!({
            "text": req.text,
            "model_id": req.model_id,
            "voice_settings": {"stability": v.stability, "similarity_boost": v.similarity_boost, "speed": v.speed},
        });
        if !req.language.is_empty() {
            body["language_code"] = req.language.clone().into();
        }
        let response = self
            .client
            .post(url)
            .header("xi-api-key", &self.cfg.api_key)
            .json(&body)
            .send()
            .await
            // reqwest errors can embed the URL; keep only the failure class.
            .map_err(|e| {
                VendorFailure::Unavailable(
                    if e.is_timeout() { "timeout" } else { "network" }.into(),
                )
            })?;
        let status = response.status();
        if status.as_u16() == 429 {
            return Err(VendorFailure::RateLimited.into());
        }
        if status.is_server_error() || status.as_u16() == 408 {
            return Err(VendorFailure::Unavailable(format!("status {}", status.as_u16())).into());
        }
        if !status.is_success() {
            return Err(VendorFailure::Permanent(format!("status {}", status.as_u16())).into());
        }
        let reply: Reply = response
            .json()
            .await
            .map_err(|_| VendorFailure::Unavailable("malformed response".into()))?;
        let bad = |m: &str| anyhow!(VendorFailure::Unavailable(m.into()));
        let audio = STANDARD
            .decode(reply.audio_base64.ok_or_else(|| bad("audio missing"))?)
            .map_err(|_| bad("audio not base64"))?;
        let raw = reply.alignment.ok_or_else(|| bad("alignment missing"))?;
        if raw.characters.len() != raw.character_start_times_seconds.len()
            || raw.characters.len() != raw.character_end_times_seconds.len()
        {
            return Err(bad("alignment lengths differ"));
        }
        let duration_ms = audio_duration_ms(&self.cfg.output_format, &audio)
            .ok_or_else(|| bad("audio duration unknown"))?;
        let start_ms = raw
            .character_start_times_seconds
            .iter()
            .map(|s| millis(*s))
            .collect::<Result<_>>()?;
        let end_ms = raw
            .character_end_times_seconds
            .iter()
            .map(|s| millis(*s))
            .collect::<Result<_>>()?;
        Ok(SpeechOutput {
            audio,
            format: if self.cfg.output_format.starts_with("wav_") {
                "audio/wav"
            } else {
                "audio/mpeg"
            }
            .into(),
            duration_ms,
            alignment: Alignment {
                characters: raw.characters,
                start_ms,
                end_ms,
            },
        })
    }
}

/// Live synthesis needs BOTH a non-empty `ELEVENLABS_API_KEY` and `NARRATION_LIVE_ENABLED=true`
/// (plus a configured `ELEVENLABS_VOICE_ID`); anything else keeps the deterministic mock.
pub fn select_speech(get: impl Fn(&str) -> Option<String>) -> Arc<dyn SpeechProvider> {
    let value = |k: &str| {
        get(k)
            .map(|v| v.trim().to_string())
            .filter(|v| !v.is_empty())
    };
    let (Some(api_key), Some(voice_id)) =
        (value("ELEVENLABS_API_KEY"), value("ELEVENLABS_VOICE_ID"))
    else {
        return Arc::new(MockSpeechProvider);
    };
    if value("NARRATION_LIVE_ENABLED").as_deref() != Some("true") {
        return Arc::new(MockSpeechProvider);
    }
    let default = VoiceConfig::resolve("default").expect("default voice");
    let number = |k: &str, d: f32| value(k).and_then(|v| v.parse().ok()).unwrap_or(d);
    Arc::new(ElevenLabsSpeech::new(ElevenLabsConfig {
        api_key,
        base_url: value("ELEVENLABS_BASE_URL").unwrap_or_else(|| DEFAULT_BASE_URL.into()),
        output_format: value("ELEVENLABS_OUTPUT_FORMAT")
            .unwrap_or_else(|| DEFAULT_OUTPUT_FORMAT.into()),
        voice: VoiceConfig {
            voice_id,
            model_id: value("ELEVENLABS_MODEL_ID").unwrap_or_else(|| DEFAULT_MODEL.into()),
            stability: number("ELEVENLABS_STABILITY", default.stability),
            similarity_boost: number("ELEVENLABS_SIMILARITY_BOOST", default.similarity_boost),
            speed: number("ELEVENLABS_SPEED", default.speed),
        },
    }))
}
