//! Preview sample slides are standalone examples (the `course_preview` structured-output schema
//! has no `topic_ids`), so the server must deliver them without the lesson topic rule.
use engine_core::{Completion, CompletionStream, LlmProvider, ProviderRouter};
use engine_proto::{
    self as pb, engine_service_client::EngineServiceClient,
    engine_service_server::EngineServiceServer,
};
use engine_server::Service;
use std::sync::Arc;
use tokio_stream::wrappers::TcpListenerStream;
use tonic::transport::Server;

struct SchemaShapedPreview;
#[tonic::async_trait]
impl LlmProvider for SchemaShapedPreview {
    fn name(&self) -> &'static str {
        "fixture"
    }
    async fn complete(&self, model: &str, input: &str) -> anyhow::Result<Completion> {
        let mut c = engine_core::MockProvider.complete(model, input).await?;
        c.text = serde_json::json!({
            "title": "Learn Rust",
            "description": "A short course",
            "units": [{"title": "Basics", "lessons": [{"title": "Variables", "goal": "Bind values"}]}],
            "slides": [{
                "html": "<div id=\"root\"><h1>Variables</h1></div>",
                "narration": "A variable binds a name to a value.",
                "duration_ms": 5000
            }]
        })
        .to_string();
        Ok(c)
    }
    async fn stream(&self, _: &str, _: &str) -> anyhow::Result<CompletionStream> {
        anyhow::bail!("not streaming")
    }
}

#[tokio::test]
async fn preview_delivers_slides_that_carry_no_topic_ids() {
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    let provider = Arc::new(SchemaShapedPreview);
    let service = EngineServiceServer::new(Service {
        router: Arc::new(ProviderRouter::new(provider.clone(), provider).unwrap()),
        speech: Arc::new(engine_core::narration::MockSpeechProvider),
        promotion: Arc::new(engine_core::promotion::Gate::for_tasks(&["course_preview"])),
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
    let response = client
        .preview_course(pb::PreviewCourseRequest {
            topic: "Rust".into(),
            limits: Some(pb::UsageLimits {
                input_tokens: 4000,
                output_tokens: 2500,
                timeout_ms: 3000,
                max_attempts: 1,
                max_cost_nanos: 100000000,
                input_nanos: 2000,
                output_nanos: 10000,
                cache_write_nanos: 2500,
                cache_read_nanos: 100,
                provider: "fixture".into(),
                fallback_provider: "fixture".into(),
                model: "tencent/hy4-flash".into(),
                fallback_model: "z-ai/glm-5.3-flash".into(),
                deadline_unix_ms: 0,
                first_token_timeout_ms: 0,
                idle_timeout_ms: 0,
            }),
            ..Default::default()
        })
        .await
        .unwrap()
        .into_inner();
    assert!(!response.unavailable);
    assert_eq!(response.example_slides.len(), 1);
    assert!(response.example_slides[0].topic_ids.is_empty());
    task.abort();
}
