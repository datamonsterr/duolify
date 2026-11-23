use super::*;
use crate::{Completion, CompletionStream, LlmProvider, Route};
use anyhow::{Result, bail};
use async_trait::async_trait;
use std::sync::{Arc, Mutex};

/// Scripted audio endpoint: each call pops the next step. `None` hangs past the attempt timeout.
type Step = Option<Result<Transcription, Option<u16>>>;
struct Scripted {
    steps: Mutex<Vec<Step>>,
    seen: Mutex<Vec<(String, usize, String)>>,
}
fn scripted(steps: Vec<Step>) -> Arc<Scripted> {
    Arc::new(Scripted {
        steps: Mutex::new(steps.into_iter().rev().collect()),
        seen: Mutex::default(),
    })
}
#[async_trait]
impl LlmProvider for Scripted {
    fn name(&self) -> &'static str {
        "scripted"
    }
    async fn complete(&self, _: &str, _: &str) -> Result<Completion> {
        bail!("chat is not used")
    }
    async fn stream(&self, _: &str, _: &str) -> Result<CompletionStream> {
        bail!("chat is not used")
    }
    async fn transcribe(
        &self,
        route: &Route,
        model: &str,
        audio: &[u8],
        container: &str,
        _: Option<&str>,
    ) -> Result<Transcription> {
        self.seen
            .lock()
            .unwrap()
            .push((model.into(), audio.len(), container.into()));
        assert!(route.zdr, "audio routes require zero data retention");
        assert_eq!(route.data_collection.as_deref(), Some("deny"));
        let step = self.steps.lock().unwrap().pop().expect("unexpected call");
        match step {
            Some(Ok(t)) => Ok(t),
            Some(Err(None)) => bail!("provider down"),
            Some(Err(Some(code))) => Err(HttpStatus(code).into()),
            None => {
                tokio::time::sleep(Duration::from_secs(60)).await;
                bail!("unreachable")
            }
        }
    }
}

fn said(text: &str, cost_nanos: u64) -> Step {
    Some(Ok(Transcription {
        text: text.into(),
        language: "en".into(),
        seconds: 4.0,
        cost_nanos,
        cost_reported: true,
        confidence: None,
        transport: None,
        model: String::new(),
    }))
}
/// A recording whose cost the provider did not report.
fn said_unpriced(text: &str, seconds: f64) -> Step {
    Some(Ok(Transcription {
        text: text.into(),
        seconds,
        cost_reported: false,
        ..Default::default()
    }))
}
/// Encoded sound: no repeating packets.
fn speech_like() -> Vec<u8> {
    let mut x = 0x9E37_79B9_7F4A_7C15u64;
    (0..4096)
        .map(|_| {
            x ^= x << 13;
            x ^= x >> 7;
            x ^= x << 17;
            (x >> 24) as u8
        })
        .collect()
}
/// Digital silence: the same packet over and over.
fn silent() -> Vec<u8> {
    [0x78u8, 0x01, 0x02].repeat(1500)
}
fn router(p: Arc<Scripted>, f: Arc<Scripted>) -> ProviderRouter {
    ProviderRouter::new(p, f).unwrap()
}
fn rec(audio: &[u8]) -> Recording<'_> {
    Recording {
        audio,
        container: "webm",
        duration_ms: 4000,
        language_hint: "",
    }
}

#[test]
fn route_is_configured_for_private_audio() {
    let r = router(scripted(vec![]), scripted(vec![]));
    let task = &r.models.tasks["transcribe"];
    assert_eq!(task.model, "qwen/qwen3-asr-0.6b");
    assert_eq!(task.fallback_model, "openai/whisper-large-v3-turbo");
    assert!(task.zdr);
    assert_eq!(task.data_collection.as_deref(), Some("deny"));
    assert_eq!(
        task.endpoint.as_deref(),
        Some("/api/v1/audio/transcriptions")
    );
}

#[test]
fn validation_rejects_before_any_provider_call() {
    let ok = [1u8; 10];
    let bad = |r: Recording<'_>| validate(&r).unwrap_err();
    assert_eq!(bad(rec(&[])), TranscribeError::InvalidAudio("empty"));
    let big = vec![1u8; MAX_AUDIO_BYTES + 1];
    assert_eq!(bad(rec(&big)), TranscribeError::InvalidAudio("too_large"));
    let mut r = rec(&ok);
    r.duration_ms = 30_001;
    assert_eq!(bad(r), TranscribeError::InvalidAudio("duration"));
    r.duration_ms = 499;
    assert_eq!(bad(r), TranscribeError::InvalidAudio("duration"));
    r.duration_ms = 4000;
    r.container = "wav";
    assert_eq!(bad(r), TranscribeError::InvalidAudio("container"));
    assert!(validate(&rec(&ok)).is_ok());
}

#[tokio::test]
async fn primary_answers_without_touching_the_fallback() {
    let (p, f) = (scripted(vec![said("hello there", 100)]), scripted(vec![]));
    let out = transcribe(&router(p.clone(), f.clone()), &rec(&[7; 9]))
        .await
        .unwrap();
    assert_eq!(out.transcription.text, "hello there");
    assert_eq!(out.transcription.model, "qwen/qwen3-asr-0.6b");
    assert!(!out.fallback_used);
    assert_eq!(out.attempts, 1);
    assert!(f.seen.lock().unwrap().is_empty());
    assert_eq!(
        p.seen.lock().unwrap()[0],
        ("qwen/qwen3-asr-0.6b".into(), 9, "webm".into())
    );
}

#[tokio::test]
async fn a_failed_primary_tries_the_fallback_exactly_once() {
    let (p, f) = (
        scripted(vec![Some(Err(None))]),
        scripted(vec![said("second try", 10)]),
    );
    let out = transcribe(&router(p, f), &rec(&[7; 9])).await.unwrap();
    assert!(out.fallback_used);
    assert_eq!(out.attempts, 2);
    assert_eq!(out.transcription.model, "openai/whisper-large-v3-turbo");

    let (p, f) = (
        scripted(vec![Some(Err(None))]),
        scripted(vec![Some(Err(None))]),
    );
    let err = transcribe(&router(p, f), &rec(&[7; 9])).await.unwrap_err();
    assert_eq!(err, TranscribeError::Failed);
    assert_eq!(err.reason(), "transcribe_failed");
}

#[tokio::test(start_paused = true)]
async fn each_attempt_has_a_hard_timeout() {
    let (p, f) = (
        scripted(vec![None]),
        scripted(vec![said("late but fine", 5)]),
    );
    let out = transcribe(&router(p, f), &rec(&[7; 9])).await.unwrap();
    assert!(out.fallback_used);
    assert!(out.latency_ms >= 8000 && out.latency_ms < 16_000);

    let (p, f) = (scripted(vec![None]), scripted(vec![None]));
    let err = transcribe(&router(p, f), &rec(&[7; 9])).await.unwrap_err();
    assert_eq!(err, TranscribeError::Timeout);
}

#[tokio::test]
async fn a_cost_above_the_ceiling_is_not_delivered_or_retried() {
    let (p, f) = (
        scripted(vec![said("expensive", MAX_COST_NANOS + 1)]),
        scripted(vec![]),
    );
    let err = transcribe(&router(p, f.clone()), &rec(&[7; 9]))
        .await
        .unwrap_err();
    assert_eq!(err, TranscribeError::CostCeiling);
    assert!(f.seen.lock().unwrap().is_empty());
    // Exactly at the ceiling is allowed.
    let (p, f) = (
        scripted(vec![said("fits", MAX_COST_NANOS)]),
        scripted(vec![]),
    );
    assert!(transcribe(&router(p, f), &rec(&[7; 9])).await.is_ok());
}

#[tokio::test]
async fn silence_hallucinated_by_the_fallback_becomes_empty_text() {
    let quiet = silent();
    for invented in [
        "Thank you.",
        "thank you",
        " You ",
        "Thanks for watching!",
        ".",
    ] {
        let (p, f) = (
            scripted(vec![Some(Err(None))]),
            scripted(vec![said(invented, 3)]),
        );
        let out = transcribe(&router(p, f), &rec(&quiet)).await.unwrap();
        assert_eq!(out.transcription.text, "", "{invented:?}");
    }
    // A real sentence from the fallback, or the same words from the primary, are kept.
    let (p, f) = (
        scripted(vec![Some(Err(None))]),
        scripted(vec![said("Thank you for the explanation", 3)]),
    );
    let out = transcribe(&router(p, f), &rec(&quiet)).await.unwrap();
    assert_eq!(out.transcription.text, "Thank you for the explanation");
    let (p, f) = (scripted(vec![said("Thank you.", 3)]), scripted(vec![]));
    let out = transcribe(&router(p, f), &rec(&quiet)).await.unwrap();
    assert_eq!(out.transcription.text, "Thank you.");
}

#[tokio::test]
async fn a_real_spoken_answer_is_kept_whatever_the_fallback_says() {
    let sound = speech_like();
    for spoken in ["Thank you.", "Okay", "you", "Bye", "Thanks"] {
        let (p, f) = (
            scripted(vec![Some(Err(None))]),
            scripted(vec![said(spoken, 3)]),
        );
        let out = transcribe(&router(p, f), &rec(&sound)).await.unwrap();
        assert!(out.fallback_used);
        assert_eq!(out.transcription.text, spoken, "{spoken:?}");
    }
    // Punctuation alone is never an answer.
    let (p, f) = (
        scripted(vec![Some(Err(None))]),
        scripted(vec![said(".", 3)]),
    );
    let out = transcribe(&router(p, f), &rec(&sound)).await.unwrap();
    assert_eq!(out.transcription.text, "");
}

#[tokio::test]
async fn a_low_provider_confidence_marks_the_audio_quiet() {
    let sound = speech_like();
    let mut t = Transcription {
        text: "Thank you.".into(),
        cost_reported: true,
        confidence: Some(0.2),
        ..Default::default()
    };
    let (p, f) = (
        scripted(vec![Some(Err(None))]),
        scripted(vec![Some(Ok(t.clone()))]),
    );
    let out = transcribe(&router(p, f), &rec(&sound)).await.unwrap();
    assert_eq!(out.transcription.text, "");
    t.confidence = Some(0.95);
    let (p, f) = (scripted(vec![Some(Err(None))]), scripted(vec![Some(Ok(t))]));
    let out = transcribe(&router(p, f), &rec(&sound)).await.unwrap();
    assert_eq!(out.transcription.text, "Thank you.");
}

#[tokio::test]
async fn an_unreported_cost_is_estimated_not_free() {
    // 4 s claimed: primary rate on the primary, fallback rate on the fallback.
    let (p, f) = (
        scripted(vec![said_unpriced("hi there", 4.0)]),
        scripted(vec![]),
    );
    let out = transcribe(&router(p, f), &rec(&[7; 9])).await.unwrap();
    assert_eq!(out.transcription.cost_nanos, 4 * PRIMARY_NANOS_PER_SECOND);
    let (p, f) = (
        scripted(vec![Some(Err(None))]),
        scripted(vec![said_unpriced("hi there", 4.0)]),
    );
    let out = transcribe(&router(p, f), &rec(&[7; 9])).await.unwrap();
    assert_eq!(out.transcription.cost_nanos, 4 * WORST_NANOS_PER_SECOND);
    // The provider billing more seconds than claimed raises the estimate, and the ceiling applies.
    let (p, f) = (
        scripted(vec![said_unpriced("long", 300.0)]),
        scripted(vec![]),
    );
    let err = transcribe(&router(p, f), &rec(&[7; 9])).await.unwrap_err();
    assert_eq!(err, TranscribeError::CostCeiling);
}

#[tokio::test(start_paused = true)]
async fn a_timed_out_primary_counts_as_worst_case_spend() {
    let (p, f) = (scripted(vec![None]), scripted(vec![said("late", 10_000)]));
    let out = transcribe(&router(p, f), &rec(&[7; 9])).await.unwrap();
    assert_eq!(
        out.transcription.cost_nanos,
        10_000 + 4 * PRIMARY_NANOS_PER_SECOND
    );
    // The timed-out spend plus a fallback at the ceiling is over the per-request ceiling.
    let (p, f) = (
        scripted(vec![None]),
        scripted(vec![said("late", MAX_COST_NANOS)]),
    );
    let err = transcribe(&router(p, f), &rec(&[7; 9])).await.unwrap_err();
    assert_eq!(err, TranscribeError::CostCeiling);
}

#[tokio::test]
async fn a_permanent_provider_rejection_is_not_retried() {
    for code in [400u16, 401, 404, 413, 422] {
        let (p, f) = (scripted(vec![Some(Err(Some(code)))]), scripted(vec![]));
        let err = transcribe(&router(p, f.clone()), &rec(&[7; 9]))
            .await
            .unwrap_err();
        assert_eq!(err, TranscribeError::Rejected, "{code}");
        assert_eq!(err.reason(), "transcribe_rejected");
        assert!(f.seen.lock().unwrap().is_empty(), "{code}");
    }
    for code in [408u16, 429, 500, 502, 503] {
        let (p, f) = (
            scripted(vec![Some(Err(Some(code)))]),
            scripted(vec![said("second try", 10)]),
        );
        let out = transcribe(&router(p, f), &rec(&[7; 9])).await.unwrap();
        assert!(out.fallback_used, "{code}");
    }
}

#[test]
fn the_language_hint_is_validated() {
    let mut r = rec(&[1u8; 10]);
    for ok in ["", "en", "vi", "yue", "pt-BR", "zh-Hans"] {
        r.language_hint = ok;
        assert!(validate(&r).is_ok(), "{ok:?}");
    }
    for bad in [
        "e",
        "english",
        "EN",
        "en_US",
        "en-",
        "en-B",
        "pt-BRAZIL",
        "en\n",
        "x".repeat(500).as_str(),
        "en,fr",
        "e1",
    ] {
        r.language_hint = bad;
        assert_eq!(
            validate(&r).unwrap_err(),
            TranscribeError::InvalidAudio("language_hint"),
            "{bad:?}"
        );
    }
}

#[test]
fn normalization_matches_the_golden_set_rules() {
    assert_eq!(
        normalized("  Hello,   World! It's fine. "),
        "hello world its fine"
    );
    assert_eq!(normalized("..."), "");
    assert_eq!(
        normalized("Xin chào, bạn khỏe không?"),
        "xin chào bạn khỏe không"
    );
}

#[tokio::test]
async fn mock_provider_is_deterministic_and_free() {
    let r =
        ProviderRouter::new(Arc::new(crate::MockProvider), Arc::new(crate::MockProvider)).unwrap();
    let out = transcribe(&r, &rec(&[1, 2, 3])).await.unwrap();
    assert_eq!(out.transcription.text, "mock transcript");
    assert_eq!(out.transcription.cost_nanos, 0);
    let silent = transcribe(&r, &rec(&[0; 64])).await.unwrap();
    assert_eq!(silent.transcription.text, "");
}

#[test]
fn word_error_rate_follows_the_spike_method() {
    assert_eq!(wer("the cat sat", "The cat sat."), Some(0.0));
    assert_eq!(wer("the cat sat", "the bat sat down"), Some(2.0 / 3.0));
    assert_eq!(wer("one two", ""), Some(1.0));
    assert_eq!(wer("", "anything"), None);
    assert_eq!(wer("don't stop - now", "dont stop now"), Some(0.0));
    assert_eq!(wer("xin chào bạn", "xin chào bạn"), Some(0.0));
    assert_eq!(wer("xin chào bạn", "xin chao bạn"), Some(1.0 / 3.0));
}
