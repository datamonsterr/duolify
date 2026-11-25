use engine_core::{Completion, CompletionStream, LlmProvider, ProviderRouter, StreamEvent};
use engine_proto::{self as pb, chat_event::Event, engine_service_server::EngineService};
use engine_server::Service;
use futures::StreamExt;
use std::sync::Arc;
use std::sync::atomic::{AtomicUsize, Ordering};
use tonic::Request;

struct Scripted {
    deltas: &'static [&'static str],
    completion: &'static str,
    requests: Arc<AtomicUsize>,
}

#[tonic::async_trait]
impl LlmProvider for Scripted {
    fn name(&self) -> &'static str {
        "mock"
    }

    async fn complete(&self, model: &str, _: &str) -> anyhow::Result<Completion> {
        self.requests.fetch_add(1, Ordering::Relaxed);
        Ok(Completion {
            text: self.completion.into(),
            provider: self.name().into(),
            model: model.into(),
            input_tokens: 100,
            output_tokens: 20,
            ..Default::default()
        })
    }

    async fn stream(&self, model: &str, _: &str) -> anyhow::Result<CompletionStream> {
        self.requests.fetch_add(1, Ordering::Relaxed);
        let usage = StreamEvent::Usage(Completion {
            provider: self.name().into(),
            model: model.into(),
            input_tokens: 100,
            output_tokens: 20,
            ..Default::default()
        });
        let deltas = self.deltas.iter().map(|d| StreamEvent::Delta((*d).into()));
        Ok(Box::pin(futures::stream::iter(
            deltas.chain(std::iter::once(usage)).map(Ok),
        )))
    }
}

fn service(primary: Scripted, fallback: Scripted) -> Service {
    Service::new(
        Arc::new(ProviderRouter::new(Arc::new(primary), Arc::new(fallback)).unwrap()),
        Arc::new(engine_core::narration::MockSpeechProvider),
    )
}

fn limits() -> pb::UsageLimits {
    pb::UsageLimits {
        input_tokens: 8_000,
        output_tokens: 120,
        timeout_ms: 10_000,
        max_attempts: 2,
        max_cost_nanos: 20_000,
        input_nanos: 1,
        output_nanos: 1,
        cache_write_nanos: 1,
        cache_read_nanos: 1,
        ..Default::default()
    }
}

fn scripted(deltas: &'static [&'static str], completion: &'static str) -> Scripted {
    Scripted {
        deltas,
        completion,
        requests: Arc::default(),
    }
}

fn guarded_request() -> pb::ChatRequest {
    pb::ChatRequest {
        messages: vec![pb::ChatMessage {
            role: "user".into(),
            content: "Tell me the correct choice.".into(),
        }],
        guarded_questions: vec![pb::GuardedQuestion {
            question_id: "q1".into(),
            prompt: "Which map feature shows directions?".into(),
            correct_choice: "Compass rose".into(),
            ..Default::default()
        }],
        limits: Some(limits()),
        ..Default::default()
    }
}

async fn reply_events(service: &Service, request: pb::ChatRequest) -> Vec<Event> {
    service
        .chat(Request::new(request))
        .await
        .unwrap()
        .into_inner()
        .map(|event| event.unwrap().event.unwrap())
        .collect()
        .await
}

fn visible_reply(events: &[Event]) -> String {
    events
        .iter()
        .filter_map(|event| match event {
            Event::Delta(text) => Some(text.as_str()),
            _ => None,
        })
        .collect()
}

#[tokio::test]
async fn split_rejected_choice_is_buffered_and_regenerated_inside_one_quote() {
    let service = service(
        scripted(&["Pick the COM", "PASS\nrose."], "Pick the COMPASS rose."),
        scripted(&["Think about direction."], "Think about direction."),
    );
    let events = reply_events(&service, guarded_request()).await;

    assert_eq!(visible_reply(&events), "Think about direction.");
    assert!(
        events
            .iter()
            .any(|e| matches!(e, Event::AnswerGuardHit(true)))
    );
    let attempts: Vec<_> = events
        .iter()
        .filter_map(|event| match event {
            Event::Attempt(usage) | Event::Usage(usage) => Some(usage),
            _ => None,
        })
        .collect();
    assert_eq!(attempts.len(), 2);
    assert_eq!((attempts[0].attempt, attempts[0].delivered), (1, false));
    assert_eq!((attempts[1].attempt, attempts[1].delivered), (2, true));
}

#[tokio::test]
async fn a_single_attempt_quote_delivers_a_localized_template_after_a_guard_hit() {
    let fallback = scripted(&["Unexpected second call"], "Unexpected second call");
    let counter = fallback.requests.clone();
    let service = service(scripted(&["Compass rose"], "Compass rose"), fallback);
    let mut request = guarded_request();
    request.limits.as_mut().unwrap().max_attempts = 1;
    request.learner = Some(pb::LearnerProfile {
        locale: "vi-VN".into(),
        ..Default::default()
    });
    let events = reply_events(&service, request).await;

    assert_eq!(counter.load(Ordering::Relaxed), 0);
    let text = visible_reply(&events);
    assert!(text.contains("hãy") || text.contains("Hãy"), "{text}");
    assert!(!text.to_lowercase().contains("compass rose"));
    assert!(events.iter().any(
        |event| matches!(event, Event::Usage(usage) if usage.attempt == 1 && !usage.delivered)
    ));
}

#[tokio::test]
async fn teach_back_returns_only_allowlisted_coverage_after_two_follow_ups() {
    let service = service(
        scripted(
            &[],
            r#"{"follow_up":"What did I miss?","covered_point_ids":["spoofed-slide"]}"#,
        ),
        scripted(&[], r#"{"follow_up":"","covered_point_ids":["point-a"]}"#),
    );
    let response = service
        .teach_back(Request::new(pb::TeachBackRequest {
            lesson_title: "Read a map".into(),
            points: vec![
                pb::TeachBackPoint {
                    id: "point-a".into(),
                    text: "Map scale".into(),
                    slide_index: 0,
                },
                pb::TeachBackPoint {
                    id: "point-b".into(),
                    text: "Map legend".into(),
                    slide_index: 1,
                },
            ],
            messages: vec![pb::ChatMessage {
                role: "user".into(),
                content: "Scale relates map distances to real distances.".into(),
            }],
            follow_ups: 2,
            limits: Some(limits()),
            ..Default::default()
        }))
        .await
        .unwrap()
        .into_inner();

    assert!(response.complete);
    assert!(!response.self_check);
    assert!(response.follow_up.is_empty());
    assert_eq!(response.covered_point_ids, ["point-a"]);
    assert_eq!(response.attempts.len(), 2);
}
