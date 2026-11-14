use engine_core::{Completion, CompletionStream, LlmProvider, ProviderRouter, StreamEvent};
use engine_proto::{
    self as pb, engine_service_client::EngineServiceClient,
    engine_service_server::EngineServiceServer,
};
use engine_server::Service;
use std::{
    io::Write,
    sync::{Arc, Mutex},
};
use tokio_stream::wrappers::TcpListenerStream;
use tonic::{Request, transport::Server};

#[derive(Clone)]
struct Collector(Arc<Mutex<Vec<u8>>>);
impl Write for Collector {
    fn write(&mut self, bytes: &[u8]) -> std::io::Result<usize> {
        self.0.lock().unwrap().extend_from_slice(bytes);
        Ok(bytes.len())
    }
    fn flush(&mut self) -> std::io::Result<()> {
        Ok(())
    }
}
struct PrivateProvider;
#[tonic::async_trait]
impl LlmProvider for PrivateProvider {
    fn name(&self) -> &'static str {
        "mock"
    }
    async fn complete(&self, _: &str, _: &str) -> anyhow::Result<Completion> {
        anyhow::bail!("private-provider-error Bearer token")
    }
    async fn stream(&self, _: &str, _: &str) -> anyhow::Result<CompletionStream> {
        Ok(Box::pin(futures::stream::once(async {
            tracing::info!("provider stream polled");
            Ok(StreamEvent::Delta("private-completion".into()))
        })))
    }
}
fn correlated<T>(value: T) -> Request<T> {
    let mut request = Request::new(value);
    request.metadata_mut().insert(
        "x-request-id",
        "ABCDEFGHIJKLMNOPQRSTUVWXYZ".parse().unwrap(),
    );
    request.metadata_mut().insert(
        "traceparent",
        "00-11111111111111111111111111111111-2222222222222222-01"
            .parse()
            .unwrap(),
    );
    request
        .metadata_mut()
        .insert("authorization", "Bearer private-token".parse().unwrap());
    request
}

#[tokio::test(flavor = "current_thread")]
async fn grpc_spans_correlate_failures_and_stream_polling_without_text() {
    let bytes = Arc::new(Mutex::new(Vec::new()));
    let collector = Collector(bytes.clone());
    let subscriber = tracing_subscriber::fmt()
        .json()
        .with_span_events(tracing_subscriber::fmt::format::FmtSpan::CLOSE)
        .with_writer(move || collector.clone())
        .finish();
    tracing::subscriber::set_global_default(subscriber).unwrap();
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    let provider = Arc::new(PrivateProvider);
    let service = EngineServiceServer::new(Service {
        router: Arc::new(ProviderRouter::new(provider.clone(), provider).unwrap()),
        speech: Arc::new(engine_core::narration::MockSpeechProvider),
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
    client
        .health(correlated(pb::HealthRequest {}))
        .await
        .unwrap();
    let error = client
        .preview_course(correlated(pb::PreviewCourseRequest {
            topic: "private-prompt".into(),
            learner: None,
            limits: None,
            ..Default::default()
        }))
        .await
        .unwrap_err();
    assert!(!error.message().contains("private"));
    let mut stream = client
        .chat(correlated(pb::ChatRequest::default()))
        .await
        .unwrap()
        .into_inner();
    assert!(stream.message().await.unwrap().is_some());
    // The provider sent no usage frame: the Engine closes with one bounded, flagged estimate.
    let estimate = stream.message().await.unwrap().unwrap();
    assert!(matches!(estimate.event, Some(pb::chat_event::Event::Usage(u)) if u.estimated));
    assert!(stream.message().await.unwrap().is_none());
    let before_brief = bytes.lock().unwrap().len();
    let brief = client
        .course_brief_chat(correlated(pb::CourseBriefChatRequest {
            brief: Some(pb::CourseBrief {
                goal: "private-goal".into(),
                ..Default::default()
            }),
            current_step: "topic".into(),
            answer: "private-answer".into(),
            limits: Some(pb::CourseBriefLimits {
                input_tokens: 2000,
                output_tokens: 1000,
                attempt_timeout_ms: 3000,
                max_attempts: 3,
                max_cost_nanos: 2100000,
                input_nanos_per_token: 100,
                output_nanos_per_token: 500,
            }),
            ..Default::default()
        }))
        .await
        .unwrap()
        .into_inner();
    assert!(brief.unavailable);
    assert_eq!(brief.attempts.len(), 3);
    let brief_log = String::from_utf8(bytes.lock().unwrap()[before_brief..].to_vec()).unwrap();
    let brief_records: Vec<serde_json::Value> = brief_log
        .lines()
        .map(|line| serde_json::from_str(line).unwrap())
        .collect();
    assert!(
        brief_records
            .iter()
            .any(|record| record["span"]["name"] == "engine.rpc"),
        "course brief RPC did not produce a tracing span: {brief_log}"
    );
    let text = String::from_utf8(bytes.lock().unwrap().clone()).unwrap();
    assert!(!text.contains("private"), "{text}");
    let records: Vec<serde_json::Value> = text
        .lines()
        .map(|line| serde_json::from_str(line).unwrap())
        .collect();
    let spans: Vec<_> = records
        .iter()
        .filter(|record| record["span"]["name"] == "engine.rpc")
        .collect();
    assert!(spans.len() >= 4, "{text}");
    for span in spans {
        assert_eq!(span["span"]["request_id"], "ABCDEFGHIJKLMNOPQRSTUVWXYZ");
        assert_eq!(span["span"]["trace_id"], "11111111111111111111111111111111");
        assert_eq!(span["span"]["parent_span_id"], "2222222222222222");
    }
    let poll = records
        .iter()
        .find(|r| r["fields"]["message"] == "provider stream polled")
        .unwrap();
    assert_eq!(
        poll["span"]["request_id"], "ABCDEFGHIJKLMNOPQRSTUVWXYZ",
        "stream poll escaped request span: {text}"
    );
    task.abort();
}
