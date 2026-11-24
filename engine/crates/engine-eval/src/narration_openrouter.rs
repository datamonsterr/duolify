use anyhow::{Context, Result, ensure};
use engine_core::credits::Attempt;
use engine_core::narration::{
    self, MockSpeechProvider, SpeechOutput, SpeechProvider, SpeechRequest,
};
use engine_providers::openrouter_tts::{Model, configured_speech};
use serde_json::{Value, json};
use std::time::{Duration, Instant};

const MP3: &[u8] = include_bytes!("../../../../evals/fixtures/narration.openrouter.mp3");
const RECORDINGS: &str = include_str!("../../../../evals/fixtures/narration.openrouter.json");

fn valid(text: &str, out: &SpeechOutput) -> bool {
    narration::validate(text, out).is_ok()
        && out.alignment.end_ms.last() == Some(&out.duration_ms)
        && out
            .alignment
            .end_ms
            .windows(2)
            .all(|ends| ends[0] <= ends[1])
}

pub async fn recorded(id: &str) -> Result<String> {
    let fixtures: Value = serde_json::from_str(RECORDINGS)?;
    let fixture = fixtures
        .get(id)
        .context("recorded OpenRouter narration case")?;
    let authored = fixture["text"]
        .as_str()
        .context("recorded narration text")?;
    let text = authored.repeat(fixture["repeat"].as_u64().unwrap_or(1) as usize);
    let language = fixture["language"].as_str().unwrap_or_default();
    let mut calls = 0;
    let budget = fixture["openrouter_budget"].as_bool().unwrap_or(true);
    if budget {
        for model in Model::ORDER {
            let Some(voice) = model.voice(language) else {
                continue;
            };
            let Some(chunks) = model.chunks(&text) else {
                continue;
            };
            calls += chunks.len();
            let corrupt = fixture["corrupt"].as_bool().unwrap_or(false);
            let outage = fixture["fail_model"].as_str() == Some(model.id());
            let bytes = if corrupt { b"bad audio" } else { MP3 };
            if !outage
                && let Ok(out) = narration::finish_speech_mp3s(
                    chunks.into_iter().map(|p| (p, bytes.to_vec())).collect(),
                )
            {
                return Ok(json!({
                    "provider":"openrouter","model":model.id(),"voice":voice.voice_id,
                    "timing_source":"estimated","alignment_valid":valid(&text,&out),
                    "duration_ms":out.duration_ms,"format":out.format,"calls":calls,
                    "captions_only":false,
                })
                .to_string());
            }
        }
    }
    if fixture["elevenlabs_budget"].as_bool().unwrap_or(true) {
        calls += 1;
        let req = SpeechRequest {
            text: text.clone(),
            language: language.into(),
            voice_id: "mock-default-voice".into(),
            model_id: "eleven_flash_v2_5".into(),
        };
        let out = MockSpeechProvider.synthesize(&req).await?;
        return Ok(
            json!({"provider":"elevenlabs","alignment_valid":valid(&text,&out),"calls":calls})
                .to_string(),
        );
    }
    Ok(json!({"captions_only":true,"captions_available":!text.trim().is_empty(),"charged_characters":0,"calls":calls}).to_string())
}

pub struct LiveNarration {
    pub text: String,
    pub attempts: Vec<Attempt>,
    pub cost_usd: f64,
}

pub async fn live(input: &Value, rate: u64, remaining_usd: Option<f64>) -> Result<LiveNarration> {
    let model = Model::ORDER
        .into_iter()
        .find(|m| input["openrouter_tts_model"].as_str() == Some(m.id()))
        .context("allowlisted OpenRouter narration model")?;
    let text = input["text"].as_str().context("live narration text")?;
    let language = input["language"]
        .as_str()
        .context("live narration language")?;
    ensure!(
        !text.trim().is_empty() && rate > 0,
        "live narration needs text and a priced rate"
    );
    let voice = model
        .voice(language)
        .context("live narration language not supported")?;
    let worst = (text.chars().count() as u64)
        .checked_mul(rate)
        .context("live narration cost overflow")?;
    let cost_usd = worst as f64 / 1_000_000_000.0;
    ensure!(
        remaining_usd.is_none_or(|remaining| cost_usd < remaining),
        "live narration exceeds remaining eval budget"
    );
    let provider = configured_speech(|key| std::env::var(key).ok(), model)?
        .context("OpenRouter narration credentials are missing")?;
    let request = SpeechRequest {
        text: text.into(),
        language: language.into(),
        voice_id: voice.voice_id,
        model_id: voice.model_id,
    };
    let started = Instant::now();
    let out = narration::synthesize_bounded(&provider, &request, Duration::from_secs(30), 1).await;
    let Ok((out, _)) = out else {
        return Ok(LiveNarration {
            text: json!({"delivered":false,"captions_only":true}).to_string(),
            attempts: vec![],
            cost_usd,
        });
    };
    Ok(LiveNarration {
        text:json!({"delivered":true,"provider":"openrouter","model":model.id(),"voice":request.voice_id,"timing_source":"estimated",
            "alignment_valid":valid(text,&out),"duration_ms":out.duration_ms}).to_string(),
        attempts: vec![Attempt {
            provider:"openrouter".into(),model:model.id().into(),input_tokens:0,output_tokens:0,cache_write_tokens:0,cache_read_tokens:0,
            latency_ms:started.elapsed().as_millis().min(u128::from(u32::MAX)) as u32,
            attempt:1, fallback_used:false, delivered:true,usage_unavailable:false,error_code:None,
        }],
        cost_usd,
    })
}
