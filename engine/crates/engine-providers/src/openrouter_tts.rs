//! OpenRouter Audio Speech transport. Narration inputs always use private routing.
use anyhow::{Result, anyhow, bail};
use async_trait::async_trait;
use engine_core::narration::{
    MAX_AUDIO_BYTES, MAX_TTS_TIMEOUT_MS, SpeechOutput, SpeechProvider, SpeechRequest,
    VendorFailure, VoiceConfig, finish_speech_mp3s, speech_chunks,
};
use reqwest::{Client, Response, Url, header};
use serde::Deserialize;
use serde_json::{Value, json};
use std::{
    collections::BTreeMap,
    sync::{Arc, OnceLock},
    time::Duration,
};

pub const PROVIDER: &str = "openrouter";
const DIRECT: &str = "https://openrouter.ai/api/v1";
const MAX_CHUNKS: usize = 5;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Model {
    Kokoro,
    Mai,
}

#[derive(Deserialize)]
struct ModelConfig {
    chunk_bytes: usize,
    providers: Vec<String>,
    voices: BTreeMap<String, String>,
}

impl Model {
    pub const ORDER: [Self; 2] = [Self::Kokoro, Self::Mai];

    pub fn id(self) -> &'static str {
        match self {
            Self::Kokoro => "hexgrad/kokoro-82m",
            Self::Mai => "microsoft/mai-voice-2.1-flash",
        }
    }

    fn config(self) -> &'static ModelConfig {
        static MODELS: OnceLock<BTreeMap<String, ModelConfig>> = OnceLock::new();
        &MODELS.get_or_init(|| {
            serde_json::from_str(include_str!("../../../config/narration-openrouter.json"))
                .expect("embedded OpenRouter narration config")
        })[self.id()]
    }

    pub fn voice(self, language: &str) -> Option<VoiceConfig> {
        let tag = language.trim().to_lowercase();
        // The Course brief can carry a display name or a serialized language tag.
        let tag = match tag.as_str() {
            "english" => "en",
            "tiếng việt" | "vietnamese" => "vi",
            "spanish" | "español" => "es",
            "japanese" | "日本語" => "ja",
            "french" | "français" => "fr",
            "portuguese" | "português" => "pt",
            "chinese" | "中文" => "zh",
            "hindi" | "हिन्दी" | "हिंदी" => "hi",
            "italian" | "italiano" => "it",
            "german" | "deutsch" => "de",
            "korean" | "한국어" => "ko",
            "russian" | "русский" => "ru",
            "dutch" | "nederlands" => "nl",
            "polish" | "polski" => "pl",
            "turkish" | "türkçe" => "tr",
            "thai" | "ไทย" => "th",
            "indonesian" | "bahasa indonesia" => "id",
            _ => tag.as_str(),
        };
        let mut subtags = tag.split('-');
        let primary = subtags.next()?;
        if !subtags.all(|part| {
            !part.is_empty() && part.len() <= 8 && part.bytes().all(|b| b.is_ascii_alphanumeric())
        }) {
            return None;
        }
        let voices = &self.config().voices;
        let voice = voices.get(tag).or_else(|| voices.get(primary))?;
        Some(VoiceConfig {
            voice_id: if self == Self::Mai {
                format!("{voice}:MAI-Voice-2.1-Flash")
            } else {
                voice.clone()
            },
            model_id: self.id().into(),
            stability: 0.0,
            similarity_boost: 0.0,
            speed: 1.0,
        })
    }

    pub fn chunks(self, text: &str) -> Option<Vec<&str>> {
        speech_chunks(text, self.config().chunk_bytes).filter(|parts| parts.len() <= MAX_CHUNKS)
    }
}

struct Gateway {
    endpoint: Url,
    token: Option<header::HeaderValue>,
}

pub struct OpenRouterSpeech {
    client: Client,
    direct: Url,
    gateway: Option<Gateway>,
    token: header::HeaderValue,
    model: Model,
}

fn token(value: &str) -> Result<header::HeaderValue> {
    if value.is_empty() || !value.bytes().all(|b| b.is_ascii_graphic()) {
        bail!("invalid OpenRouter narration credential");
    }
    let mut value = header::HeaderValue::from_str(&format!("Bearer {value}"))
        .map_err(|_| anyhow!("invalid OpenRouter narration credential"))?;
    value.set_sensitive(true);
    Ok(value)
}

fn endpoint(base: &str) -> Result<Url> {
    let mut url = Url::parse(base).map_err(|_| anyhow!("invalid OPENROUTER_BASE_URL"))?;
    let loopback = matches!(url.host_str(), Some("localhost" | "127.0.0.1" | "[::1]"));
    if !url.has_host()
        || !(url.scheme() == "https" || loopback && url.scheme() == "http")
        || !url.username().is_empty()
        || url.password().is_some()
        || url.query().is_some()
        || url.fragment().is_some()
    {
        bail!("invalid OPENROUTER_BASE_URL");
    }
    if url.host_str() == Some("gateway.ai.cloudflare.com") {
        let path: Vec<_> = url
            .path_segments()
            .into_iter()
            .flatten()
            .filter(|p| !p.is_empty())
            .collect();
        if !matches!(path.as_slice(), ["v1", _, _, "openrouter"]) {
            bail!("OPENROUTER_BASE_URL must point to the OpenRouter Gateway provider path");
        }
    }
    url.path_segments_mut()
        .map_err(|()| anyhow!("invalid OPENROUTER_BASE_URL"))?
        .pop_if_empty()
        .extend(["audio", "speech"]);
    Ok(url)
}

impl OpenRouterSpeech {
    pub fn new(
        direct: &str,
        gateway: Option<(&str, Option<&str>)>,
        api_key: &str,
        model: Model,
    ) -> Result<Self> {
        let gateway = gateway
            .map(|(base, key)| -> Result<Gateway> {
                let endpoint = endpoint(base)?;
                if endpoint.host_str() == Some("openrouter.ai") {
                    bail!("the Gateway base must use a separate host");
                }
                Ok(Gateway {
                    endpoint,
                    token: key.map(token).transpose()?,
                })
            })
            .transpose()?;
        Ok(Self {
            client: Client::builder()
                .redirect(reqwest::redirect::Policy::none())
                .retry(reqwest::retry::never())
                .timeout(Duration::from_millis(u64::from(MAX_TTS_TIMEOUT_MS)))
                .build()?,
            direct: endpoint(direct)?,
            token: token(api_key)?,
            gateway,
            model,
        })
    }

    async fn post(&self, body: &Value, gateway: Option<&Gateway>) -> reqwest::Result<Response> {
        let url = gateway.map_or(&self.direct, |g| &g.endpoint);
        let mut request = self
            .client
            .post(url.clone())
            .header(header::AUTHORIZATION, self.token.clone())
            .json(body);
        if let Some(gateway) = gateway {
            request = request
                .header("cf-aig-collect-log", "false")
                .header("cf-aig-skip-cache", "true")
                .header("cf-aig-max-attempts", "1")
                .header("cf-aig-no-wholesale", "true");
            if let Some(key) = &gateway.token {
                request = request.header("cf-aig-authorization", key.clone());
            }
        }
        request.send().await
    }

    async fn mp3(&self, input: &str, voice: &str) -> Result<Vec<u8>> {
        let providers = &self.model.config().providers;
        let body = json!({
            "model": self.model.id(),
            "input": input,
            "voice": voice,
            "response_format": "mp3",
            "provider": {
                "order": providers, "only": providers, "sort": "price", "allow_fallbacks": true,
                "data_collection": "deny", "zdr": true,
            },
        });
        let result = self.post(&body, self.gateway.as_ref()).await;
        // Direct recovery is limited to an identifiable failure before vendor submission.
        // An ambiguous gateway status or body may already have incurred spend.
        let mut response = match result {
            Ok(mut response) => {
                if self.gateway.is_some() && gateway_cannot_route(&mut response).await {
                    self.post(&body, None).await
                } else {
                    Ok(response)
                }
            }
            Err(ref error) if self.gateway.is_some() && (error.is_connect() || error.is_dns()) => {
                self.post(&body, None).await
            }
            other => other,
        }
        .map_err(|_| VendorFailure::Unavailable("speech transport failure".into()))?;
        let status = response.status();
        if !status.is_success() {
            let cause = format!("status {}", status.as_u16());
            return Err(if status.is_server_error() || status.as_u16() == 408 {
                VendorFailure::Unavailable(cause)
            } else {
                VendorFailure::Permanent(cause)
            }
            .into());
        }
        let invalid = || VendorFailure::Permanent("invalid OpenRouter MP3 response".into());
        let mime = response
            .headers()
            .get(header::CONTENT_TYPE)
            .and_then(|h| h.to_str().ok())
            .and_then(|h| h.split(';').next());
        if !mime.is_some_and(|h| h.trim().eq_ignore_ascii_case("audio/mpeg"))
            || response
                .content_length()
                .is_some_and(|n| n > MAX_AUDIO_BYTES as u64)
        {
            return Err(invalid().into());
        }
        let mut audio = Vec::new();
        while let Some(chunk) = response
            .chunk()
            .await
            .map_err(|_| VendorFailure::Unavailable("speech read failed".into()))?
        {
            if chunk.len() > MAX_AUDIO_BYTES - audio.len() {
                return Err(invalid().into());
            }
            audio.extend_from_slice(&chunk);
        }
        Ok(audio)
    }
}

async fn gateway_cannot_route(response: &mut Response) -> bool {
    if response.status().as_u16() != 400 {
        return false;
    }
    let mut raw = Vec::new();
    while let Ok(Some(part)) = response.chunk().await {
        if part.len() > 4096 - raw.len() {
            return false;
        }
        raw.extend_from_slice(&part);
    }
    serde_json::from_slice::<Value>(&raw).is_ok_and(|body| {
        body["error"]["code"].as_u64() == Some(2005)
            || body["errors"]
                .as_array()
                .is_some_and(|items| items.iter().any(|e| e["code"].as_u64() == Some(2005)))
    })
}

#[async_trait]
impl SpeechProvider for OpenRouterSpeech {
    fn name(&self) -> &'static str {
        PROVIDER
    }
    fn routed_voice(&self, key: &str, language: &str) -> Option<VoiceConfig> {
        (key == "default")
            .then(|| self.model.voice(language))
            .flatten()
    }
    fn audio_format(&self) -> &'static str {
        "audio/mpeg"
    }
    fn timing_source(&self) -> &'static str {
        "estimated"
    }

    async fn synthesize(&self, req: &SpeechRequest) -> Result<SpeechOutput> {
        if req.model_id != self.model.id()
            || self
                .model
                .voice(&req.language)
                .is_none_or(|voice| voice.voice_id != req.voice_id)
        {
            return Err(
                VendorFailure::Permanent("unapproved narration model or voice".into()).into(),
            );
        }
        let chunks = self
            .model
            .chunks(&req.text)
            .ok_or_else(|| VendorFailure::Permanent("invalid speech input length".into()))?;
        let mut parts = Vec::with_capacity(chunks.len());
        for chunk in chunks {
            parts.push((chunk, self.mp3(chunk, &req.voice_id).await?));
        }
        finish_speech_mp3s(parts)
            .map_err(|_| VendorFailure::Permanent("invalid OpenRouter MP3 audio".into()).into())
    }
}

/// The RPC resolves the ordered candidates and synthesizes one authorized model at a time.
pub struct SpeechRoutes {
    elevenlabs: Arc<dyn SpeechProvider>,
    openrouter: Vec<OpenRouterSpeech>,
}

impl SpeechRoutes {
    pub fn new(elevenlabs: Arc<dyn SpeechProvider>, openrouter: Vec<OpenRouterSpeech>) -> Self {
        Self {
            elevenlabs,
            openrouter,
        }
    }
}

#[async_trait]
impl SpeechProvider for SpeechRoutes {
    fn name(&self) -> &'static str {
        self.elevenlabs.name()
    }
    fn voice(&self, key: &str) -> Option<VoiceConfig> {
        self.elevenlabs.voice(key)
    }
    fn audio_format(&self) -> &'static str {
        self.elevenlabs.audio_format()
    }
    fn alternate(&self, language: &str, model: &str) -> Option<&dyn SpeechProvider> {
        self.openrouter
            .iter()
            .find(|p| {
                (model.is_empty() || p.model.id() == model) && p.model.voice(language).is_some()
            })
            .map(|p| p as &dyn SpeechProvider)
    }
    fn alternate_models(&self, language: &str) -> Vec<&'static str> {
        self.openrouter
            .iter()
            .filter(|p| p.model.voice(language).is_some())
            .map(|p| p.model.id())
            .collect()
    }
    async fn synthesize(&self, req: &SpeechRequest) -> Result<SpeechOutput> {
        self.elevenlabs.synthesize(req).await
    }
}

struct UnconfiguredElevenLabs;

#[async_trait]
impl SpeechProvider for UnconfiguredElevenLabs {
    fn name(&self) -> &'static str {
        "elevenlabs"
    }
    fn voice(&self, _: &str) -> Option<VoiceConfig> {
        None
    }
    async fn synthesize(&self, _: &SpeechRequest) -> Result<SpeechOutput> {
        Err(VendorFailure::Permanent("ElevenLabs unconfigured".into()).into())
    }
}

pub fn configured_speech(
    get: impl Fn(&str) -> Option<String>,
    model: Model,
) -> Result<Option<OpenRouterSpeech>> {
    let Some(key) = get("OPENROUTER_API_KEY").filter(|key| !key.trim().is_empty()) else {
        return Ok(None);
    };
    let base = get("OPENROUTER_BASE_URL").unwrap_or_default();
    let base = base.trim().trim_end_matches('/');
    let gateway_key = get("AI_GATEWAY_TOKEN").filter(|key| !key.is_empty());
    let gateway = if base.is_empty() || base == DIRECT {
        if gateway_key.is_some() {
            bail!("AI_GATEWAY_TOKEN requires a Gateway OPENROUTER_BASE_URL");
        }
        None
    } else {
        Some((base, gateway_key.as_deref()))
    };
    OpenRouterSpeech::new(DIRECT, gateway, key.trim(), model).map(Some)
}

pub fn select_speech(get: impl Fn(&str) -> Option<String>) -> Result<Arc<dyn SpeechProvider>> {
    let eleven = crate::elevenlabs::select_speech(&get);
    let selection = get("NARRATION_PROVIDER").unwrap_or_default();
    match selection.trim() {
        "elevenlabs" => Ok(eleven),
        "" | PROVIDER if get("NARRATION_LIVE_ENABLED").as_deref() != Some("true") => Ok(eleven),
        "" | PROVIDER => {
            let mut openrouter = Vec::new();
            for model in Model::ORDER {
                openrouter.extend(configured_speech(&get, model)?);
            }
            let fallback = if eleven.name() == "mock" {
                Arc::new(UnconfiguredElevenLabs)
            } else {
                eleven
            };
            Ok(Arc::new(SpeechRoutes::new(fallback, openrouter)))
        }
        _ => bail!("NARRATION_PROVIDER must be openrouter or elevenlabs"),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn narration_defaults_to_openrouter_with_live_credentials_and_checks_language() {
        let env = |key: &str| match key {
            "OPENROUTER_API_KEY" => Some("fixture-key".into()),
            "NARRATION_LIVE_ENABLED" => Some("true".into()),
            _ => None,
        };
        let routed = select_speech(env).unwrap();
        assert_eq!(
            routed.alternate_models("en-GB"),
            [Model::Kokoro.id(), Model::Mai.id()]
        );
        assert_eq!(routed.alternate_models("Tiếng Việt"), [Model::Mai.id()]);
        assert!(routed.alternate_models("ar").is_empty());
        assert!(routed.alternate_models("en--GB").is_empty());
        assert!(
            select_speech(|key| if key == "NARRATION_PROVIDER" {
                Some("elevenlabs".into())
            } else {
                env(key)
            })
            .unwrap()
            .alternate_models("en")
            .is_empty()
        );
        assert!(
            select_speech(|key| if key == "NARRATION_PROVIDER" {
                Some("unknown".into())
            } else {
                env(key)
            })
            .is_err()
        );
    }
}
