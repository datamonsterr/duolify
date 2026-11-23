use super::*;
use crate::{Completion, CompletionStream, LlmProvider, Route};
use anyhow::{Result, bail};
use async_trait::async_trait;
use std::sync::{Arc, Mutex};

/// Scripted audio endpoint: each call pops the next step. `None` hangs past the attempt timeout.
struct Scripted {
    steps: Mutex<Vec<Option<Result<Transcription, ()>>>>,
    seen: Mutex<Vec<(String, usize, String)>>,
}
fn scripted(steps: Vec<Option<Result<Transcription, ()>>>) -> Arc<Scripted> {
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
            Some(Err(())) => bail!("provider down"),
            None => {
                tokio::time::sleep(Duration::from_secs(60)).await;
                bail!("unreachable")
            }
        }
    }
}

fn said(text: &str, cost_nanos: u64) -> Option<Result<Transcription, ()>> {
    Some(Ok(Transcription {
        text: text.into(),
        language: "en".into(),
        seconds: 4.0,
        cost_nanos,
        model: String::new(),
    }))
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
        scripted(vec![Some(Err(()))]),
        scripted(vec![said("second try", 10)]),
    );
    let out = transcribe(&router(p, f), &rec(&[7; 9])).await.unwrap();
    assert!(out.fallback_used);
    assert_eq!(out.attempts, 2);
    assert_eq!(out.transcription.model, "openai/whisper-large-v3-turbo");

    let (p, f) = (scripted(vec![Some(Err(()))]), scripted(vec![Some(Err(()))]));
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
    for invented in [
        "Thank you.",
        "thank you",
        " You ",
        "Thanks for watching!",
        ".",
    ] {
        let (p, f) = (
            scripted(vec![Some(Err(()))]),
            scripted(vec![said(invented, 3)]),
        );
        let out = transcribe(&router(p, f), &rec(&[7; 9])).await.unwrap();
        assert_eq!(out.transcription.text, "", "{invented:?}");
    }
    // A real sentence from the fallback, or the same words from the primary, are kept.
    let (p, f) = (
        scripted(vec![Some(Err(()))]),
        scripted(vec![said("Thank you for the explanation", 3)]),
    );
    let out = transcribe(&router(p, f), &rec(&[7; 9])).await.unwrap();
    assert_eq!(out.transcription.text, "Thank you for the explanation");
    let (p, f) = (scripted(vec![said("Thank you.", 3)]), scripted(vec![]));
    let out = transcribe(&router(p, f), &rec(&[7; 9])).await.unwrap();
    assert_eq!(out.transcription.text, "Thank you.");
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
