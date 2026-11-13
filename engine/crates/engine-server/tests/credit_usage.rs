use engine_core::{Completion, CompletionStream, LlmProvider, ProviderRouter};
use engine_proto::{
    self as pb, engine_service_client::EngineServiceClient,
    engine_service_server::EngineServiceServer,
};
use engine_server::Service;
use std::sync::Arc;
use tokio_stream::wrappers::TcpListenerStream;
use tonic::transport::Server;

struct MissingUsage;
#[tonic::async_trait]
impl LlmProvider for MissingUsage {
    fn name(&self) -> &'static str {
        "fixture"
    }
    async fn complete(&self, model: &str, input: &str) -> anyhow::Result<Completion> {
        let completion = engine_core::MockProvider.complete(model, input).await?;
        let mut value = serde_json::to_value(completion)?;
        value["usage_unavailable"] = true.into();
        Ok(serde_json::from_value(value)?)
    }
    async fn stream(&self, _: &str, _: &str) -> anyhow::Result<CompletionStream> {
        anyhow::bail!("not streaming")
    }
}

#[tokio::test]
async fn preview_preserves_missing_usage_through_grpc() {
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    let provider = Arc::new(MissingUsage);
    let service = EngineServiceServer::new(Service {
        router: Arc::new(ProviderRouter::new(provider.clone(), provider).unwrap()),
    });
    let task = tokio::spawn(async move {
        Server::builder()
            .add_service(service)
            .serve_with_incoming(TcpListenerStream::new(listener))
            .await
            .unwrap();
    });
    let mut client = EngineServiceClient::connect(format!("http://{addr}"))
        .await
        .unwrap();
    let mut request = pb::PreviewCourseRequest {
        topic: "fixture".into(),
        limits: Some(pb::UsageLimits {
            input_tokens: 4000,
            output_tokens: 2500,
            timeout_ms: 3000,
            max_attempts: 2,
            max_cost_nanos: 100000000,
            input_nanos: 2000,
            output_nanos: 10000,
            cache_write_nanos: 2500,
            cache_read_nanos: 100,
            provider: "fixture".into(),
            fallback_provider: "fixture".into(),
            model: "claude-sonnet-5-5".into(),
            fallback_model: "anthropic/claude-sonnet-5.5".into(),
            deadline_unix_ms: 0,
            first_token_timeout_ms: 0,
            idle_timeout_ms: 0,
        }),
        ..Default::default()
    };
    let response = client
        .preview_course(request.clone())
        .await
        .unwrap()
        .into_inner();
    assert!(!response.unavailable);
    assert_eq!(response.attempts.len(), 1);
    assert!(response.attempts[0].delivered);
    assert!(response.attempts[0].usage_unavailable);
    request.limits.as_mut().unwrap().deadline_unix_ms = 1;
    let expired = client.preview_course(request).await.unwrap().into_inner();
    assert!(expired.unavailable);
    assert!(expired.attempts.is_empty());
    task.abort();
}

#[tokio::test]
async fn enrich_note_returns_real_attempts_and_enforces_limits() {
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    let provider = Arc::new(engine_core::MockProvider);
    let service = EngineServiceServer::new(Service {
        router: Arc::new(ProviderRouter::new(provider.clone(), provider).unwrap()),
    });
    let task = tokio::spawn(async move {
        Server::builder()
            .add_service(service)
            .serve_with_incoming(TcpListenerStream::new(listener))
            .await
            .unwrap();
    });
    let mut client = EngineServiceClient::connect(format!("http://{addr}"))
        .await
        .unwrap();

    let request = pb::EnrichNoteRequest {
        note: "my note".into(),
        lesson: None,
        learner: None,
        limits: Some(pb::UsageLimits {
            input_tokens: 4000,
            output_tokens: 2500,
            timeout_ms: 3000,
            max_attempts: 2,
            max_cost_nanos: 100000000,
            input_nanos: 1000,
            output_nanos: 5000,
            cache_write_nanos: 1250,
            cache_read_nanos: 100,
            provider: "mock".into(),
            fallback_provider: "mock".into(),
            model: "claude-haiku-5-5".into(),
            fallback_model: "anthropic/claude-haiku-5.5".into(),
            deadline_unix_ms: 0,
            first_token_timeout_ms: 0,
            idle_timeout_ms: 0,
        }),
        allowed_lessons: vec![],
    };

    let response = client
        .enrich_note(request.clone())
        .await
        .unwrap()
        .into_inner();
    assert!(!response.unavailable);
    assert_eq!(
        response.attempts.len(),
        1,
        "enrich_note must return real attempts, not empty"
    );
    assert!(response.attempts[0].delivered);
    assert_eq!(response.attempts[0].provider, "mock");

    // Route mismatch check
    let mut mismatch_req = request.clone();
    mismatch_req.limits.as_mut().unwrap().model = "wrong-model".into();
    let mismatch_err = client.enrich_note(mismatch_req).await;
    assert!(mismatch_err.is_err());

    task.abort();
}
