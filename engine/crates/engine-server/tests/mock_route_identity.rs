//! Mock mode serves the same route identity as live (provider name and models.toml model ids),
//! so the API's live quote runs end to end without paid calls; unknown routes stay refused.
use engine_core::{LlmProvider, ProviderRouter};
use engine_proto::{self as pb, engine_service_server::EngineService};
use engine_server::Service;
use std::sync::Arc;
use tonic::{Code, Request};

const HY4: &str = "tencent/hy4-flash";
const GLM: &str = "z-ai/glm-5.3-flash";

fn service(router: ProviderRouter) -> Service {
    Service {
        router: Arc::new(router),
        speech: Arc::new(engine_core::narration::MockSpeechProvider),
        promotion: Default::default(),
    }
}

fn live() -> ProviderRouter {
    // Nothing listens here: a refused quote must fail before any provider call.
    let p: Arc<dyn LlmProvider> = Arc::new(engine_providers::HttpProvider::openrouter(
        "unused".into(),
        "http://127.0.0.1:9".into(),
    ));
    ProviderRouter::new(p.clone(), p).unwrap()
}

fn preview(model: &str) -> Request<pb::PreviewCourseRequest> {
    Request::new(pb::PreviewCourseRequest {
        brief: Some(pb::CourseBrief {
            topic: "Go programming".into(),
            ..Default::default()
        }),
        limits: Some(pb::UsageLimits {
            input_tokens: 16000,
            output_tokens: 8000,
            timeout_ms: 30000,
            max_attempts: 2,
            max_cost_nanos: 1_000_000_000,
            input_nanos: 100,
            output_nanos: 500,
            cache_write_nanos: 125,
            cache_read_nanos: 10,
            provider: "openrouter".into(),
            model: model.into(),
            fallback_provider: "openrouter".into(),
            fallback_model: GLM.into(),
            ..Default::default()
        }),
        ..Default::default()
    })
}

#[tokio::test]
async fn mock_mode_accepts_the_live_route_quote() {
    let svc = service(engine_providers::router("mock").unwrap());
    let response = svc.preview_course(preview(HY4)).await.unwrap().into_inner();
    assert!(!response.unavailable);
    assert!(response.sample_outline.is_some());
    assert_eq!(response.attempts[0].provider, "openrouter");
    assert_eq!(response.attempts[0].model, HY4);
}

#[tokio::test]
async fn unknown_model_is_refused_in_mock_and_live_mode() {
    let mut live = service(live());
    live.promotion = Arc::new(engine_core::promotion::Gate::for_tasks(&["course_preview"]));
    for svc in [service(engine_providers::router("mock").unwrap()), live] {
        let err = svc
            .preview_course(preview("unknown/model"))
            .await
            .unwrap_err();
        assert_eq!(err.code(), Code::FailedPrecondition);
    }
}

#[tokio::test]
async fn a_mock_provider_name_is_no_longer_a_valid_quote() {
    let mut request = preview(HY4);
    let limits = request.get_mut().limits.as_mut().unwrap();
    limits.provider = "mock".into();
    let err = service(engine_providers::router("mock").unwrap())
        .preview_course(request)
        .await
        .unwrap_err();
    assert_eq!(err.code(), Code::FailedPrecondition);
}

#[tokio::test]
async fn health_reports_the_configured_route_per_task() {
    let svc = service(engine_providers::router("mock").unwrap());
    let providers = svc
        .health(Request::new(pb::HealthRequest {}))
        .await
        .unwrap()
        .into_inner()
        .providers;
    assert_eq!(providers[..2], ["openrouter", "openrouter"]);
    assert!(
        providers.contains(&format!("route:preview=openrouter/{HY4},openrouter/{GLM}")),
        "{providers:?}"
    );
    // Review F1: the quote identity looks live, so Health also states the transport.
    assert_eq!(providers[2], "mode:fixture");
    assert_eq!(providers.len(), 3 + 16);
}

#[tokio::test]
async fn fixture_only_screens_stay_closed_on_the_live_router() {
    let request = || {
        Request::new(pb::ScreenInstructionRequest {
            text: "More examples about pointers".into(),
            course_topic: "Go".into(),
        })
    };
    let mock = service(engine_providers::router("mock").unwrap());
    assert!(
        mock.screen_instruction(request())
            .await
            .unwrap()
            .into_inner()
            .allowed
    );
    let err = service(live())
        .screen_instruction(request())
        .await
        .unwrap_err();
    assert_eq!(err.code(), Code::Unavailable);
}
