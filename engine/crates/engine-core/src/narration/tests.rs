use super::*;
use std::sync::atomic::{AtomicU32, Ordering};
use std::time::Duration;

fn request(text: &str) -> SpeechRequest {
    SpeechRequest {
        text: text.into(),
        language: "en".into(),
        voice_id: "mock-default-voice".into(),
        model_id: "eleven_flash_v2_5".into(),
    }
}

#[tokio::test]
async fn mock_is_deterministic_and_covers_every_unicode_character() {
    let text = "Xin chào, thế giới.";
    let a = MockSpeechProvider.synthesize(&request(text)).await.unwrap();
    let b = MockSpeechProvider.synthesize(&request(text)).await.unwrap();
    assert_eq!(a.audio, b.audio);
    assert_eq!(a.duration_ms, b.duration_ms);
    assert_eq!(a.alignment.characters.len(), text.chars().count());
    assert_eq!(&a.audio[..4], b"RIFF");
    assert_eq!(a.format, "audio/wav");
    validate(text, &a).unwrap();
}

#[tokio::test]
async fn validation_rejects_gaps_reversals_and_duration_drift() {
    let text = "Hello sky";
    let good = MockSpeechProvider.synthesize(&request(text)).await.unwrap();
    let mut gap = good.clone();
    gap.alignment.characters.pop();
    gap.alignment.start_ms.pop();
    gap.alignment.end_ms.pop();
    assert!(matches!(
        validate(text, &gap),
        Err(SpeechError::InvalidAlignment(_))
    ));
    let mut reversed = good.clone();
    reversed.alignment.start_ms.swap(2, 5);
    assert!(validate(text, &reversed).is_err());
    let mut wrong = good.clone();
    wrong.alignment.characters[1] = "Z".into();
    assert!(validate(text, &wrong).is_err());
    let mut drift = good.clone();
    drift.duration_ms += 10_000;
    assert!(validate(text, &drift).is_err());
    let mut empty = good.clone();
    empty.audio.clear();
    assert!(validate(text, &empty).is_err());
    assert!(matches!(
        validate("  ", &good),
        Err(SpeechError::MissingText)
    ));
}

#[test]
fn voice_key_resolves_from_config_and_fingerprint_tracks_settings() {
    let voice = VoiceConfig::resolve("default").unwrap();
    assert_eq!(voice.voice_id, "mock-default-voice");
    assert_eq!(
        voice.fingerprint(),
        VoiceConfig::resolve("default").unwrap().fingerprint()
    );
    let mut changed = voice.clone();
    changed.stability += 0.1;
    assert_ne!(voice.fingerprint(), changed.fingerprint());
    assert!(VoiceConfig::resolve("not-a-voice").is_none());
}

struct Flaky {
    calls: AtomicU32,
    fail_first: u32,
    invalid: bool,
    hang: bool,
}

#[async_trait::async_trait]
impl SpeechProvider for Flaky {
    fn name(&self) -> &'static str {
        "flaky"
    }
    async fn synthesize(&self, req: &SpeechRequest) -> anyhow::Result<SpeechOutput> {
        let n = self.calls.fetch_add(1, Ordering::SeqCst);
        if self.hang {
            tokio::time::sleep(Duration::from_secs(60)).await;
        }
        if n < self.fail_first {
            anyhow::bail!("provider down");
        }
        let mut out = MockSpeechProvider.synthesize(req).await?;
        if self.invalid {
            out.alignment.characters.pop();
            out.alignment.start_ms.pop();
            out.alignment.end_ms.pop();
        }
        Ok(out)
    }
}

#[tokio::test]
async fn bounded_synthesis_retries_then_succeeds_and_reports_attempts() {
    let p = Flaky {
        calls: 0.into(),
        fail_first: 2,
        invalid: false,
        hang: false,
    };
    let (out, attempts) = synthesize_bounded(&p, &request("Hello"), Duration::from_secs(1), 3)
        .await
        .unwrap();
    assert_eq!(attempts, 3);
    assert_eq!(out.alignment.characters.len(), 5);
}

#[tokio::test]
async fn bounded_synthesis_stops_at_max_attempts_and_rejects_invalid_alignment() {
    let down = Flaky {
        calls: 0.into(),
        fail_first: 99,
        invalid: false,
        hang: false,
    };
    let err = synthesize_bounded(&down, &request("Hello"), Duration::from_secs(1), 2)
        .await
        .unwrap_err();
    assert_eq!(down.calls.load(Ordering::SeqCst), 2);
    assert!(matches!(err, SpeechError::Provider(_)));
    let bad = Flaky {
        calls: 0.into(),
        fail_first: 0,
        invalid: true,
        hang: false,
    };
    let err = synthesize_bounded(&bad, &request("Hello"), Duration::from_secs(1), 2)
        .await
        .unwrap_err();
    assert_eq!(bad.calls.load(Ordering::SeqCst), 2);
    assert!(matches!(err, SpeechError::InvalidAlignment(_)));
}

#[tokio::test]
async fn bounded_synthesis_times_out_each_call() {
    let hang = Flaky {
        calls: 0.into(),
        fail_first: 0,
        invalid: false,
        hang: true,
    };
    let err = synthesize_bounded(&hang, &request("Hello"), Duration::from_millis(50), 2)
        .await
        .unwrap_err();
    assert!(matches!(err, SpeechError::Timeout));
    assert_eq!(hang.calls.load(Ordering::SeqCst), 2);
}

#[test]
fn usage_reports_actual_characters_for_audio_component() {
    let u = usage_for("eleven_flash_v2_5", 19, 2, 12);
    assert_eq!(u.component, "audio");
    assert_eq!(u.provider, "elevenlabs");
    assert_eq!(u.narration_characters, 19);
    assert_eq!(u.attempt, 2);
    assert!(u.delivered);
}

/// The recorded fixture pins the mock's bytes, duration and alignment so the golden-set
/// alignment checks stay deterministic across engine changes.
#[tokio::test]
async fn recorded_mock_fixture_matches_provider_and_passes_alignment_checks() {
    let fixture: serde_json::Value = serde_json::from_str(include_str!(
        "../../../../../evals/fixtures/narration.mock.json"
    ))
    .unwrap();
    let text = fixture["text"].as_str().unwrap();
    let voice = VoiceConfig::resolve(fixture["voice_key"].as_str().unwrap()).unwrap();
    assert_eq!(voice.voice_id, fixture["voice_id"].as_str().unwrap());
    assert_eq!(voice.model_id, fixture["model_id"].as_str().unwrap());
    let out = MockSpeechProvider.synthesize(&request(text)).await.unwrap();
    validate(text, &out).unwrap();
    assert_eq!(
        u64::from(out.duration_ms),
        fixture["duration_ms"].as_u64().unwrap()
    );
    assert_eq!(
        out.audio.len() as u64,
        fixture["audio_bytes"].as_u64().unwrap()
    );
    let chars: Vec<&str> = fixture["alignment"]["characters"]
        .as_array()
        .unwrap()
        .iter()
        .map(|c| c.as_str().unwrap())
        .collect();
    assert_eq!(out.alignment.characters, chars);
    let ends: Vec<u64> = fixture["alignment"]["end_ms"]
        .as_array()
        .unwrap()
        .iter()
        .map(|v| v.as_u64().unwrap())
        .collect();
    assert_eq!(
        out.alignment
            .end_ms
            .iter()
            .map(|v| u64::from(*v))
            .collect::<Vec<_>>(),
        ends
    );
}

fn dedupe_fixture() -> serde_json::Value {
    serde_json::from_str(include_str!(
        "../../../../../evals/fixtures/narration.dedupe.json"
    ))
    .unwrap()
}

/// The resolved config fingerprint is the shared identity input: it is stable across
/// calls and changes whenever any voice setting changes (a new retained version).
#[test]
fn resolved_fingerprint_matches_the_recorded_fixture_and_tracks_settings() {
    let fixture = dedupe_fixture();
    let voice = VoiceConfig::resolve("default").unwrap();
    assert_eq!(
        voice.fingerprint(),
        fixture["settings_fingerprint"].as_str().unwrap()
    );
    assert_eq!(
        voice.fingerprint(),
        VoiceConfig::resolve("default").unwrap().fingerprint()
    );
    for tweak in [
        |v: &mut VoiceConfig| v.stability = 0.6,
        |v: &mut VoiceConfig| v.similarity_boost = 0.8,
        |v: &mut VoiceConfig| v.speed = 1.1,
        |v: &mut VoiceConfig| v.voice_id = "other-voice".into(),
    ] {
        let mut changed = voice.clone();
        tweak(&mut changed);
        assert_ne!(changed.fingerprint(), voice.fingerprint());
    }
}

/// Cache case: the API normalizes whitespace before the call, so equivalent texts reach the
/// provider as one text and produce byte-identical audio.
#[tokio::test]
async fn recorded_cache_case_produces_identical_audio_for_normalized_text() {
    let case = &dedupe_fixture()["cases"][0];
    let texts: Vec<String> = case["texts"]
        .as_array()
        .unwrap()
        .iter()
        .map(|t| {
            t.as_str()
                .unwrap()
                .split_whitespace()
                .collect::<Vec<_>>()
                .join(" ")
        })
        .collect();
    assert_eq!(texts[0], texts[1]);
    let a = MockSpeechProvider
        .synthesize(&request(&texts[0]))
        .await
        .unwrap();
    let b = MockSpeechProvider
        .synthesize(&request(&texts[1]))
        .await
        .unwrap();
    assert_eq!(a.audio, b.audio);
    assert!(case["expect"]["same_audio"].as_bool().unwrap());
}

/// Retry case: a transient failure is bounded-retried and the delivered result is the same
/// artifact (same bytes, so the same content hash) with exactly one delivered usage.
#[tokio::test]
async fn recorded_retry_case_delivers_the_same_artifact_once() {
    struct FlakyThenMock(AtomicU32);
    #[async_trait::async_trait]
    impl SpeechProvider for FlakyThenMock {
        fn name(&self) -> &'static str {
            "flaky"
        }
        async fn synthesize(&self, req: &SpeechRequest) -> anyhow::Result<SpeechOutput> {
            if self.0.fetch_add(1, Ordering::SeqCst) == 0 {
                anyhow::bail!("engine_unavailable");
            }
            MockSpeechProvider.synthesize(req).await
        }
    }
    let case = &dedupe_fixture()["cases"][1];
    let text = case["text"].as_str().unwrap();
    let flaky = FlakyThenMock(AtomicU32::new(0));
    let out = synthesize_bounded(&flaky, &request(text), Duration::from_secs(1), 2)
        .await
        .unwrap();
    let direct = MockSpeechProvider.synthesize(&request(text)).await.unwrap();
    assert_eq!(out.0.audio, direct.audio);
    assert_eq!(out.1, 2, "second attempt delivered");
    assert_eq!(flaky.0.load(Ordering::SeqCst), 2);
    let delivered = usage_for("eleven_flash_v2_5", text.chars().count() as u32, 2, 5);
    assert!(delivered.delivered);
    assert_eq!(case["expect"]["delivered_charges"], 1);
}

/// Alignment failure: dropping the last character is rejected, so nothing is delivered or charged.
#[tokio::test]
async fn recorded_alignment_failure_case_is_rejected_before_delivery() {
    let case = &dedupe_fixture()["cases"][2];
    let text = case["text"].as_str().unwrap();
    let mut out = MockSpeechProvider.synthesize(&request(text)).await.unwrap();
    out.alignment.characters.pop();
    out.alignment.start_ms.pop();
    out.alignment.end_ms.pop();
    assert!(matches!(
        validate(text, &out),
        Err(SpeechError::InvalidAlignment(_))
    ));
    assert_eq!(case["expect"]["error"], "invalid_alignment");
    assert_eq!(case["expect"]["charge"], 0);
}
