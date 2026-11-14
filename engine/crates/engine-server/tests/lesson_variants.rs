use engine_core::{Completion, CompletionStream, LlmProvider, ProviderRouter};
use engine_proto::{self as pb, engine_service_server::EngineService};
use engine_server::Service;
use std::sync::{Arc, Mutex};
use tonic::Request;

struct Recorded {
    outputs: Mutex<std::collections::VecDeque<String>>,
    calls: Mutex<Vec<String>>,
}
#[tonic::async_trait]
impl LlmProvider for Recorded {
    fn name(&self) -> &'static str {
        "mock"
    }
    async fn complete(&self, model: &str, input: &str) -> anyhow::Result<Completion> {
        self.calls.lock().unwrap().push(input.into());
        let text = self
            .outputs
            .lock()
            .unwrap()
            .pop_front()
            .unwrap_or_else(|| "{}".into());
        Ok(Completion {
            text,
            provider: "mock".into(),
            model: model.into(),
            input_tokens: 1,
            output_tokens: 1,
            cache_write_tokens: 0,
            cache_read_tokens: 0,
            usage_unavailable: false,
        })
    }
    async fn stream(&self, _: &str, _: &str) -> anyhow::Result<CompletionStream> {
        anyhow::bail!("unused")
    }
}
fn source() -> pb::Question {
    pb::Question {
        question_id: "q1".into(),
        topic_id: "stars".into(),
        prompt: "What makes a star shine?".into(),
        choices: vec!["Fusion".into(), "Reflection".into()],
        answer_index: 0,
        explanation: "Fusion produces light.".into(),
        variant_of: String::new(),
    }
}
fn limits() -> pb::UsageLimits {
    pb::UsageLimits {
        input_tokens: 8000,
        output_tokens: 14000,
        timeout_ms: 3000,
        max_attempts: 2,
        max_cost_nanos: 500_000_000,
        input_nanos: 2000,
        output_nanos: 10000,
        cache_write_nanos: 2500,
        cache_read_nanos: 100,
        provider: "mock".into(),
        fallback_provider: "mock".into(),
        model: "claude-sonnet-5-5".into(),
        fallback_model: "anthropic/claude-sonnet-5.5".into(),
        ..Default::default()
    }
}
fn variant() -> String {
    serde_json::json!({"variants":[{"question_id":"v1","topic_id":"stars","variant_of":"q1","prompt":"Which process powers the light emitted by stars?","choices":["Reflection","Fusion"],"answer_index":1,"explanation":"Fusion produces starlight."}]}).to_string()
}
fn service(outputs: Vec<String>) -> (Service, Arc<Recorded>) {
    let provider = Arc::new(Recorded {
        outputs: Mutex::new(outputs.into()),
        calls: Mutex::new(vec![]),
    });
    (
        Service {
            router: Arc::new(ProviderRouter::new(provider.clone(), provider.clone()).unwrap()),
            speech: Arc::new(engine_core::narration::MockSpeechProvider),
        },
        provider,
    )
}
#[tokio::test]
async fn paraphrase_calls_provider_and_repairs_once() {
    let (s, p) = service(vec!["{}".into(), variant()]);
    let r = s
        .generate_retry_variants(Request::new(pb::GenerateRetryVariantsRequest {
            source: Some(source()),
            limits: Some(limits()),
            ..Default::default()
        }))
        .await
        .unwrap()
        .into_inner();
    assert!(!r.unavailable);
    assert_eq!(p.calls.lock().unwrap().len(), 2);
    assert_eq!(r.attempts.len(), 2);
    assert_eq!(
        r.variants[0].prompt,
        "Which process powers the light emitted by stars?"
    );
}
#[tokio::test]
async fn paraphrase_rejects_identical_prompt_wrong_answer_and_topic_drift() {
    for field in ["prompt", "answer_index", "topic_id"] {
        let mut bad: serde_json::Value = serde_json::from_str(&variant()).unwrap();
        bad["variants"][0][field] = match field {
            "prompt" => serde_json::json!(source().prompt),
            "answer_index" => serde_json::json!(0),
            _ => serde_json::json!("untaught"),
        };
        let (s, p) = service(vec![bad.to_string(), bad.to_string(), variant()]);
        let r = s
            .generate_retry_variants(Request::new(pb::GenerateRetryVariantsRequest {
                source: Some(source()),
                limits: Some(limits()),
                ..Default::default()
            }))
            .await
            .unwrap()
            .into_inner();
        assert!(r.unavailable, "accepted invalid {field}");
        assert!(r.variants.is_empty());
        assert_eq!(p.calls.lock().unwrap().len(), 2);
    }
}
#[tokio::test]
async fn missing_topic_is_never_defaulted_in_lesson_live_path() {
    let lesson = serde_json::json!({"slides":[{"slide_id":"s1","html":"<div id=\"root\">Stars</div>","narration":"Stars shine.","topic_ids":["top-1"],"duration_ms":3000}],"test":[{"question_id":"q1","prompt":"Why?","choices":["Fusion","Reflection"],"answer_index":0,"explanation":"Fusion"}]});
    let (s, _) = service(vec![lesson.to_string(), lesson.to_string()]);
    match s
        .generate_lesson(Request::new(pb::GenerateLessonRequest {
            limits: Some(limits()),
            ..Default::default()
        }))
        .await
    {
        Err(_) => (),
        Ok(r) => {
            use futures::StreamExt;
            let mut stream = r.into_inner();
            while let Some(event) = stream.next().await {
                if let Ok(pb::GenerateLessonEvent {
                    event: Some(pb::generate_lesson_event::Event::Lesson(_)),
                }) = event
                {
                    panic!("missing topic accepted and defaulted")
                }
            }
        }
    }
}
#[tokio::test]
async fn lesson_without_pool_calls_real_retry_path() {
    use futures::StreamExt;
    let lesson = serde_json::json!({"slides":[{"slide_id":"s1","html":"<div id=\"root\">Stars</div>","narration":"Stars shine.","topic_ids":["stars"],"duration_ms":3000}],"test":[{"question_id":"q1","topic_id":"stars","prompt":source().prompt,"choices":source().choices,"answer_index":0,"explanation":"Fusion produces light."}]});
    let (s, p) = service(vec![lesson.to_string(), variant()]);
    let mut stream = s
        .generate_lesson(Request::new(pb::GenerateLessonRequest {
            limits: Some(limits()),
            ..Default::default()
        }))
        .await
        .unwrap()
        .into_inner();
    let mut delivered = false;
    while let Some(event) = stream.next().await {
        if let Some(pb::generate_lesson_event::Event::Lesson(l)) = event.unwrap().event {
            assert_eq!(
                l.variant_pool[0].variants[0].prompt,
                "Which process powers the light emitted by stars?"
            );
            delivered = true;
        }
    }
    assert!(delivered);
    assert_eq!(p.calls.lock().unwrap().len(), 2);
}
