//! Deterministic (mock provider) mode must serve a valid answer for every model-backed RPC the
//! API calls: the full-stack flow suite and CI run on it. The mock picks its fixture from the
//! request; a prompt that merely mentions another task's words must not get that task's fixture.
use engine_proto::{self as pb, engine_service_server::EngineService};
use engine_server::Service;
use futures::StreamExt;
use std::sync::Arc;
use tonic::Request;

fn service() -> Service {
    Service {
        router: Arc::new(engine_providers::router("mock").unwrap()),
        speech: Arc::new(engine_core::narration::MockSpeechProvider),
        promotion: Default::default(),
    }
}

fn limits(output_tokens: u32) -> pb::UsageLimits {
    pb::UsageLimits {
        input_tokens: 18000,
        output_tokens,
        timeout_ms: 10000,
        max_attempts: 2,
        max_cost_nanos: 500_000_000,
        input_nanos: 2000,
        output_nanos: 10000,
        cache_write_nanos: 2500,
        cache_read_nanos: 100,
        provider: "openrouter".into(),
        fallback_provider: "openrouter".into(),
        model: "tencent/hy4-flash".into(),
        fallback_model: "z-ai/glm-5.3-flash".into(),
        ..Default::default()
    }
}

#[tokio::test]
async fn generate_lesson_delivers_a_lesson_that_passes_production_validation() {
    // The lesson prompt names `variants` (LESSON_SHAPE); it must still get the lesson fixture.
    let mut stream = service()
        .generate_lesson(Request::new(pb::GenerateLessonRequest {
            lesson: Some(pb::LessonRef {
                lesson_id: "l-1".into(),
                title: "Stars".into(),
                goal: "explain why stars shine".into(),
            }),
            limits: Some(limits(14000)),
            ..Default::default()
        }))
        .await
        .unwrap()
        .into_inner();
    let mut lesson = None;
    let mut events = Vec::new();
    while let Some(event) = stream.next().await {
        let event = event.unwrap();
        events.push(
            format!("{:?}", event.event)
                .chars()
                .take(80)
                .collect::<String>(),
        );
        if let Some(pb::generate_lesson_event::Event::Lesson(l)) = event.event {
            lesson = Some(l);
        }
    }
    let lesson = lesson.unwrap_or_else(|| panic!("no lesson delivered: {events:?}"));
    assert!(!lesson.slides.is_empty() && !lesson.test.is_empty());
}

#[tokio::test]
async fn generate_retry_variants_delivers_variants() {
    let r = service()
        .generate_retry_variants(Request::new(pb::GenerateRetryVariantsRequest {
            source: Some(pb::Question {
                question_id: "q1".into(),
                topic_id: "top-stars".into(),
                prompt: "What kind of object makes its own light?".into(),
                choices: vec!["Self-luminous star".into(), "Planet".into(), "Comet".into()],
                answer_index: 0,
                explanation: "Stars make their own light by fusion.".into(),
                variant_of: String::new(),
            }),
            limits: Some(limits(2000)),
            ..Default::default()
        }))
        .await
        .unwrap()
        .into_inner();
    assert!(!r.unavailable, "{r:?}");
    assert!(!r.variants.is_empty());
}

#[tokio::test]
async fn the_safety_screens_answer_in_mock_mode() {
    let s = service();
    let steer = s
        .screen_instruction(Request::new(pb::ScreenInstructionRequest {
            text: "More examples about pointers".into(),
            course_topic: "Go".into(),
        }))
        .await
        .unwrap()
        .into_inner();
    assert!(steer.allowed, "{steer:?}");
    let course = s
        .screen_course(Request::new(pb::ScreenCourseRequest {
            sample_json: r#"{"title":"Go basics","lessons":[{"title":"Slices"}]}"#.into(),
            sampled_lessons: 1,
            limits: Some(limits(200)),
        }))
        .await
        .unwrap()
        .into_inner();
    assert_eq!(course.verdict, "pass", "{course:?}");
    let review = s
        .screen_review(Request::new(pb::ScreenReviewRequest {
            text: "Clear examples, thanks.".into(),
            // The review screen's own envelope (sharing::screen_review).
            limits: Some(pb::UsageLimits {
                input_tokens: 4000,
                max_cost_nanos: 20_000_000,
                ..limits(300)
            }),
        }))
        .await
        .unwrap()
        .into_inner();
    assert_eq!(review.verdict, "pass", "{review:?}");
}
