//! ElevenLabs adapter against a local fake server. No test reaches the live API.
use base64::{Engine as _, engine::general_purpose::STANDARD};
use engine_core::narration::{SpeechError, SpeechRequest, VoiceConfig, synthesize_bounded};
use engine_providers::elevenlabs::{ElevenLabsConfig, ElevenLabsSpeech, select_speech};
use serde_json::json;
use std::{collections::HashMap, time::Duration};
use wiremock::matchers::{header, method, path};
use wiremock::{Mock, MockServer, Request, ResponseTemplate};

const KEY: &str = "sk_fake_secret_key";
const TEXT: &str = "Xin chào sky";

fn voice() -> VoiceConfig {
    VoiceConfig {
        voice_id: "voice-abc".into(),
        model_id: "eleven_flash_v2_5".into(),
        stability: 0.5,
        similarity_boost: 0.75,
        speed: 1.0,
    }
}

fn provider(server: &MockServer) -> ElevenLabsSpeech {
    ElevenLabsSpeech::new(ElevenLabsConfig {
        api_key: KEY.into(),
        base_url: server.uri(),
        voice: voice(),
        output_format: "wav_16000".into(),
    })
}

fn request() -> SpeechRequest {
    SpeechRequest {
        text: TEXT.into(),
        language: "vi".into(),
        voice_id: "voice-abc".into(),
        model_id: "eleven_flash_v2_5".into(),
    }
}

fn wav(ms: u32) -> Vec<u8> {
    let bytes = 32 * ms; // 16 kHz, 16-bit mono
    let mut w = Vec::new();
    w.extend_from_slice(b"RIFF\0\0\0\0WAVEfmt ");
    w.extend_from_slice(&16u32.to_le_bytes());
    w.extend_from_slice(&1u16.to_le_bytes());
    w.extend_from_slice(&1u16.to_le_bytes());
    w.extend_from_slice(&16000u32.to_le_bytes());
    w.extend_from_slice(&32000u32.to_le_bytes());
    w.extend_from_slice(&2u16.to_le_bytes());
    w.extend_from_slice(&16u16.to_le_bytes());
    w.extend_from_slice(b"data");
    w.extend_from_slice(&bytes.to_le_bytes());
    w.extend(std::iter::repeat_n(0u8, bytes as usize));
    w
}

/// One 50 ms slot per character, in seconds like the vendor.
fn body(text: &str, starts_reversed: bool) -> serde_json::Value {
    let chars: Vec<String> = text.chars().map(String::from).collect();
    let n = chars.len();
    let mut starts: Vec<f64> = (0..n).map(|i| i as f64 * 0.05).collect();
    let mut ends: Vec<f64> = (1..=n).map(|i| i as f64 * 0.05).collect();
    if starts_reversed {
        starts.reverse();
        ends.reverse();
    }
    json!({
        "audio_base64": STANDARD.encode(wav(n as u32 * 50)),
        "alignment": {"characters": chars, "character_start_times_seconds": starts, "character_end_times_seconds": ends},
        "normalized_alignment": null
    })
}

fn ok(text: &str) -> ResponseTemplate {
    ResponseTemplate::new(200).set_body_json(body(text, false))
}

#[tokio::test]
async fn synthesizes_audio_and_character_alignment_from_the_timestamps_endpoint() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .and(path("/v1/text-to-speech/voice-abc/with-timestamps"))
        .and(header("xi-api-key", KEY))
        .respond_with(|req: &Request| {
            let q: HashMap<_, _> = req.url.query_pairs().into_owned().collect();
            assert_eq!(q["output_format"], "wav_16000");
            let b: serde_json::Value = serde_json::from_slice(&req.body).unwrap();
            assert_eq!(b["text"], TEXT);
            assert_eq!(b["model_id"], "eleven_flash_v2_5");
            assert_eq!(b["language_code"], "vi");
            assert_eq!(b["voice_settings"]["stability"], 0.5);
            assert_eq!(b["voice_settings"]["speed"], 1.0);
            ok(TEXT)
        })
        .expect(1)
        .mount(&server)
        .await;
    let (out, attempt) =
        synthesize_bounded(&provider(&server), &request(), Duration::from_secs(5), 2)
            .await
            .unwrap();
    assert_eq!(attempt, 1);
    assert_eq!(out.format, "audio/wav");
    assert_eq!(out.alignment.characters.len(), TEXT.chars().count());
    assert_eq!(out.alignment.start_ms[1], 50);
    assert_eq!(out.duration_ms, TEXT.chars().count() as u32 * 50);
}

#[tokio::test]
async fn a_rate_limit_is_retried_once_then_succeeds() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .respond_with(ResponseTemplate::new(429).set_body_string("quota for sk_fake_secret_key"))
        .up_to_n_times(1)
        .mount(&server)
        .await;
    Mock::given(method("POST"))
        .respond_with(ok(TEXT))
        .mount(&server)
        .await;
    let (_, attempt) =
        synthesize_bounded(&provider(&server), &request(), Duration::from_secs(5), 2)
            .await
            .unwrap();
    assert_eq!(attempt, 2);
}

#[tokio::test]
async fn persistent_429_is_rate_limited_and_stops_after_the_retry() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .respond_with(ResponseTemplate::new(429))
        .expect(2)
        .mount(&server)
        .await;
    let err = synthesize_bounded(&provider(&server), &request(), Duration::from_secs(5), 2)
        .await
        .unwrap_err();
    assert!(matches!(err, SpeechError::RateLimited));
}

#[tokio::test]
async fn server_errors_are_unavailable_and_never_echo_the_key_or_body() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .respond_with(
            ResponseTemplate::new(503).set_body_string("down: sk_fake_secret_key Xin chào"),
        )
        .expect(2)
        .mount(&server)
        .await;
    let err = synthesize_bounded(&provider(&server), &request(), Duration::from_secs(5), 2)
        .await
        .unwrap_err();
    let shown = format!("{err} {err:?}");
    assert!(matches!(err, SpeechError::Provider(_)));
    assert!(
        !shown.contains(KEY) && !shown.contains("Xin chào"),
        "{shown}"
    );
}

#[tokio::test]
async fn authentication_and_validation_errors_are_permanent() {
    for status in [401u16, 422] {
        let server = MockServer::start().await;
        Mock::given(method("POST"))
            .respond_with(ResponseTemplate::new(status))
            .expect(1)
            .mount(&server)
            .await;
        let err = synthesize_bounded(&provider(&server), &request(), Duration::from_secs(5), 2)
            .await
            .unwrap_err();
        assert!(matches!(err, SpeechError::Provider(_)), "{status}");
    }
}

#[tokio::test]
async fn malformed_json_missing_alignment_and_bad_audio_are_rejected() {
    for payload in [
        ResponseTemplate::new(200).set_body_string("not json"),
        ResponseTemplate::new(200).set_body_json(json!({"audio_base64": "!!", "alignment": null})),
        ResponseTemplate::new(200)
            .set_body_json(json!({"audio_base64": STANDARD.encode(wav(600)), "alignment": null})),
    ] {
        let server = MockServer::start().await;
        Mock::given(method("POST"))
            .respond_with(payload)
            .expect(2)
            .mount(&server)
            .await;
        assert!(
            synthesize_bounded(&provider(&server), &request(), Duration::from_secs(5), 2)
                .await
                .is_err()
        );
    }
}

#[tokio::test]
async fn decreasing_wrong_or_short_alignment_is_invalid_after_one_repair() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .respond_with(ResponseTemplate::new(200).set_body_json(body(TEXT, true)))
        .expect(2)
        .mount(&server)
        .await;
    let err = synthesize_bounded(&provider(&server), &request(), Duration::from_secs(5), 2)
        .await
        .unwrap_err();
    assert!(matches!(err, SpeechError::InvalidAlignment(_)));

    // Alignment for different characters than were sent is rejected too.
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .respond_with(ok("Other text!"))
        .expect(2)
        .mount(&server)
        .await;
    let err = synthesize_bounded(&provider(&server), &request(), Duration::from_secs(5), 2)
        .await
        .unwrap_err();
    assert!(matches!(err, SpeechError::InvalidAlignment(_)));
}

#[tokio::test]
async fn a_slow_vendor_times_out_and_cancels_the_request() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .respond_with(ok(TEXT).set_delay(Duration::from_secs(5)))
        .mount(&server)
        .await;
    let started = std::time::Instant::now();
    let err = synthesize_bounded(
        &provider(&server),
        &request(),
        Duration::from_millis(100),
        1,
    )
    .await
    .unwrap_err();
    assert!(matches!(err, SpeechError::Timeout));
    assert!(started.elapsed() < Duration::from_secs(2));
}

#[test]
fn debug_output_redacts_the_api_key() {
    let p = ElevenLabsSpeech::new(ElevenLabsConfig {
        api_key: KEY.into(),
        base_url: "http://127.0.0.1:1".into(),
        voice: voice(),
        output_format: "wav_16000".into(),
    });
    assert!(!format!("{p:?}").contains(KEY));
}

fn env<'a>(pairs: &'a [(&'a str, &'a str)]) -> impl Fn(&str) -> Option<String> + 'a {
    move |k| {
        pairs
            .iter()
            .find(|(n, _)| *n == k)
            .map(|(_, v)| (*v).to_string())
    }
}

#[test]
fn live_mode_requires_both_a_key_and_the_explicit_flag() {
    let live = [
        ("ELEVENLABS_API_KEY", KEY),
        ("NARRATION_LIVE_ENABLED", "true"),
        ("ELEVENLABS_VOICE_ID", "voice-abc"),
    ];
    assert_eq!(select_speech(env(&live)).name(), "elevenlabs");
    assert_eq!(
        select_speech(env(&live[..1])).name(),
        "mock",
        "key alone stays mock"
    );
    assert_eq!(
        select_speech(env(&live[1..])).name(),
        "mock",
        "flag alone stays mock"
    );
    let off = [
        ("ELEVENLABS_API_KEY", KEY),
        ("NARRATION_LIVE_ENABLED", "false"),
        ("ELEVENLABS_VOICE_ID", "v"),
    ];
    assert_eq!(select_speech(env(&off)).name(), "mock");
    let blank = [
        ("ELEVENLABS_API_KEY", " "),
        ("NARRATION_LIVE_ENABLED", "true"),
        ("ELEVENLABS_VOICE_ID", "v"),
    ];
    assert_eq!(select_speech(env(&blank)).name(), "mock");
    let no_voice = [
        ("ELEVENLABS_API_KEY", KEY),
        ("NARRATION_LIVE_ENABLED", "true"),
    ];
    assert_eq!(
        select_speech(env(&no_voice)).name(),
        "mock",
        "no configured voice stays mock"
    );
    assert_eq!(
        select_speech(env(&[])).voice("default").unwrap().voice_id,
        "mock-default-voice"
    );
    assert_eq!(
        select_speech(env(&live)).voice("default").unwrap().voice_id,
        "voice-abc"
    );
}

// ---- recorded fixture replay (evals/fixtures/narration.vendor.json) --------------------

fn vendor_fixture() -> serde_json::Value {
    serde_json::from_str(include_str!(
        "../../../../evals/fixtures/narration.vendor.json"
    ))
    .unwrap()
}
fn case(id: &str) -> serde_json::Value {
    vendor_fixture()["cases"]
        .as_array()
        .unwrap()
        .iter()
        .find(|c| c["id"] == id)
        .unwrap()
        .clone()
}
fn req_for(text: &str) -> SpeechRequest {
    SpeechRequest {
        text: text.into(),
        ..request()
    }
}

#[tokio::test]
async fn recorded_multilingual_case_aligns_every_character() {
    for text in case("multilingual")["texts"].as_array().unwrap() {
        let text = text.as_str().unwrap();
        let server = MockServer::start().await;
        Mock::given(method("POST"))
            .respond_with(ok(text))
            .mount(&server)
            .await;
        let (out, _) = synthesize_bounded(
            &provider(&server),
            &req_for(text),
            Duration::from_secs(5),
            2,
        )
        .await
        .unwrap();
        assert_eq!(out.alignment.characters.concat(), text);
        assert_eq!(out.alignment.characters.len(), text.chars().count());
    }
}

#[tokio::test]
async fn recorded_vendor_cases_replay_status_sequences_within_the_attempt_cap() {
    for id in [
        "vendor-rate-limit-then-ok",
        "vendor-outage",
        "vendor-auth-rejected",
        "timing-reversed",
    ] {
        let c = case(id);
        let text = c["text"].as_str().unwrap();
        let server = MockServer::start().await;
        if id == "timing-reversed" {
            Mock::given(method("POST"))
                .respond_with(ResponseTemplate::new(200).set_body_json(body(text, true)))
                .mount(&server)
                .await;
        } else {
            for status in c["responses"].as_array().unwrap() {
                let reply = match status.as_u64().unwrap() {
                    200 => ok(text),
                    s => ResponseTemplate::new(s as u16),
                };
                Mock::given(method("POST"))
                    .respond_with(reply)
                    .up_to_n_times(1)
                    .mount(&server)
                    .await;
            }
        }
        let result = synthesize_bounded(
            &provider(&server),
            &req_for(text),
            Duration::from_secs(5),
            2,
        )
        .await;
        let seen = server.received_requests().await.unwrap().len() as u64;
        let expect = &c["expect"];
        assert_eq!(seen, expect["attempts"].as_u64().unwrap(), "{id}");
        match expect["error"].as_str() {
            None => assert!(result.is_ok(), "{id}"),
            Some("invalid_alignment") => assert!(
                matches!(result, Err(SpeechError::InvalidAlignment(_))),
                "{id}"
            ),
            Some(_) => assert!(matches!(result, Err(SpeechError::Provider(_))), "{id}"),
        }
    }
}
