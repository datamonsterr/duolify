//! E17-08: narration of pinned-Cast dialogue segments. Reuses the E14 speech pipeline; this
//! module only resolves a Character's logical voice key through the approved voice config,
//! bounds budgets, and names fixed content-free fallback events. No provider ids come from callers.
use crate::narration::{VOICES_JSON, VoiceConfig};
use serde::Deserialize;
use std::collections::BTreeMap;

/// The safe allowlisted voice used when a Character's voice is retired or unavailable.
pub const DEFAULT_VOICE: &str = "default";
/// E17-08 ceilings, stricter than or equal to E14's (60 s, 2 attempts): 30 s per call, one retry.
pub const TTS_TIMEOUT_MS: u32 = 30_000;
pub const TTS_ATTEMPTS: u32 = 2;
/// Fixed fallback reason codes (never free text).
pub const REASONS: [&str; 3] = ["voice_retired", "voice_unavailable", "voice_not_entitled"];

#[derive(Debug, Clone, PartialEq)]
pub struct ResolvedVoice {
    /// The logical voice actually used.
    pub voice_key: String,
    pub config: VoiceConfig,
    /// Set when the requested voice was replaced by the default.
    pub fallback: Option<&'static str>,
}

#[derive(Deserialize)]
struct Entry {
    key: String,
    voice_id: String,
    model_id: String,
    stability: f32,
    similarity_boost: f32,
    speed: f32,
    #[serde(default)]
    retired: bool,
}

#[derive(Deserialize)]
struct File {
    #[serde(default)]
    version: String,
    voices: Vec<Entry>,
}

/// The approved versioned voice config.
pub struct CharacterVoices {
    version: String,
    voices: BTreeMap<String, (VoiceConfig, bool)>,
}

impl CharacterVoices {
    /// None when the config does not hold the default voice.
    pub fn from_json(raw: &str) -> Option<Self> {
        let file: File = serde_json::from_str(raw).ok()?;
        let voices: BTreeMap<_, _> = file
            .voices
            .into_iter()
            .map(|v| {
                let config = VoiceConfig {
                    voice_id: v.voice_id,
                    model_id: v.model_id,
                    stability: v.stability,
                    similarity_boost: v.similarity_boost,
                    speed: v.speed,
                };
                (v.key, (config, v.retired))
            })
            .collect();
        voices.contains_key(DEFAULT_VOICE).then_some(Self {
            version: file.version,
            voices,
        })
    }

    pub fn version(&self) -> &str {
        &self.version
    }

    /// Resolves a logical key. Unknown keys (including raw provider ids) and retired voices use the default.
    pub fn resolve(&self, key: &str) -> ResolvedVoice {
        let pick = |k: &str, fallback| {
            let (config, _) = &self.voices[k];
            ResolvedVoice {
                voice_key: k.into(),
                config: config.clone(),
                fallback,
            }
        };
        match self.voices.get(key) {
            Some((_, false)) => pick(key, None),
            Some((_, true)) => pick(DEFAULT_VOICE, Some("voice_retired")),
            None => pick(DEFAULT_VOICE, Some("voice_unavailable")),
        }
    }
}

/// Embedded engine config.
pub fn voices() -> CharacterVoices {
    CharacterVoices::from_json(VOICES_JSON).expect("embedded voice config holds the default voice")
}

pub fn resolve_voice(key: &str) -> ResolvedVoice {
    voices().resolve(key)
}

/// Stable segment id: the Slide id plus the segment's position, so order is part of identity.
pub fn segment_id(slide_id: &str, index: usize) -> String {
    format!("{slide_id}__s{index}")
}

/// Accepts (segment id, text) pairs only in their original zero-based order for the Slide.
pub fn segments_in_order(slide_id: &str, segments: &[(String, String)]) -> Result<(), String> {
    for (i, (id, _)) in segments.iter().enumerate() {
        if *id != segment_id(slide_id, i) {
            return Err("segment order changed".into());
        }
    }
    Ok(())
}

/// Per-call timeout and attempts: the request may only tighten the character ceilings.
pub fn budget(timeout_ms: u32, attempts: u32) -> (u32, u32) {
    let clamp = |asked: u32, max: u32| if asked == 0 { max } else { asked.min(max) };
    (
        clamp(timeout_ms, TTS_TIMEOUT_MS),
        clamp(attempts, TTS_ATTEMPTS),
    )
}

/// Content-free `character_voice_fallback` properties: bounded speaker key, fixed reason, config version.
pub fn fallback_event(
    speaker_key: &str,
    reason: &str,
    config_version: &str,
) -> BTreeMap<&'static str, String> {
    let key_ok = !speaker_key.is_empty()
        && speaker_key.len() <= 24
        && speaker_key
            .chars()
            .all(|c| c.is_ascii_lowercase() || c.is_ascii_digit() || c == '-' || c == '_');
    BTreeMap::from([
        (
            "speaker_key",
            if key_ok { speaker_key } else { "unknown" }.to_string(),
        ),
        (
            "reason_code",
            if REASONS.contains(&reason) {
                reason
            } else {
                "voice_unavailable"
            }
            .to_string(),
        ),
        (
            "voice_config_version",
            config_version.chars().take(32).collect(),
        ),
    ])
}
