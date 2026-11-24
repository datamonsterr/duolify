use engine_core::{ProviderRouter, promotion::Gate};
use engine_proto::{
    self as pb, chat_event::Event, engine_service_client::EngineServiceClient,
    engine_service_server::EngineServiceServer, open_router_transport as transport,
};
use engine_providers::{GatewayConfig, HttpProvider};
use engine_server::Service;
use serde_json::{Value, json};
use std::sync::Arc;
use tokio_stream::wrappers::TcpListenerStream;
use tonic::transport::{Channel, Server};
use wiremock::{Mock, MockServer, ResponseTemplate, matchers::path};

const GATEWAY_PATH: &str = "/v1/account/engine/openrouter";
const CHAT_PATH: &str = "/v1/account/engine/openrouter/chat/completions";
const DIRECT_CHAT_PATH: &str = "/api/v1/chat/completions";

fn gateway_case(name: &str) -> Value {
    let cases: Value = serde_json::from_str(include_str!(
        "../../../../evals/fixtures/openrouter-gateway.json"
    ))
    .unwrap();
    cases[name].clone()
}

async fn serve(
    direct: &MockServer,
    gateway: Option<&MockServer>,
) -> (EngineServiceClient<Channel>, tokio::task::JoinHandle<()>) {
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    let gateway_base = gateway.map(|server| format!("{}{GATEWAY_PATH}", server.uri()));
    let config = GatewayConfig::parse(
        gateway_base.as_deref(),
        gateway.map(|_| "gateway-token"),
        None,
    )
    .unwrap();
    let provider = Arc::new(
        HttpProvider::openrouter_with_gateway(
            "openrouter-key".into(),
            format!("{}{DIRECT_CHAT_PATH}", direct.uri()),
            config,
        )
        .unwrap(),
    );
    let service = EngineServiceServer::new(Service {
        router: Arc::new(ProviderRouter::new(provider.clone(), provider).unwrap()),
        speech: Arc::new(engine_core::narration::MockSpeechProvider),
        promotion: Arc::new(Gate::for_tasks(&[
            "tutor_chat",
            "note_enrich",
            "transcribe",
        ])),
    });
    let task = tokio::spawn(async move {
        Server::builder()
            .add_service(service)
            .serve_with_incoming(TcpListenerStream::new(listener))
            .await
            .unwrap();
    });
    let client = EngineServiceClient::connect(format!("http://{addr}"))
        .await
        .unwrap();
    (client, task)
}

fn chat(text: &str) -> ResponseTemplate {
    ResponseTemplate::new(200).set_body_raw(
        format!(
            "data: {}\n\ndata: {}\n\ndata: [DONE]\n\n",
            json!({"choices": [{"delta": {"content": text}}]}),
            json!({"usage": {"prompt_tokens": 10, "completion_tokens": 5}})
        ),
        "text/event-stream",
    )
}

async fn chat_events(client: &mut EngineServiceClient<Channel>) -> Vec<Event> {
    let mut stream = client
        .chat(pb::ChatRequest::default())
        .await
        .unwrap()
        .into_inner();
    let mut events = Vec::new();
    while let Some(event) = stream.message().await.unwrap() {
        events.push(event.event.unwrap());
    }
    events
}

#[tokio::test]
async fn gateway_502_recovers_over_grpc_and_reports_direct_fallback_usage() {
    let gateway = MockServer::start().await;
    let direct = MockServer::start().await;
    let case = gateway_case("stream_before_delta");
    let delta = case["direct_delta"].as_str().unwrap();
    Mock::given(path(CHAT_PATH))
        .respond_with(ResponseTemplate::new(
            case["gateway_status"].as_u64().unwrap() as u16,
        ))
        .mount(&gateway)
        .await;
    Mock::given(path(DIRECT_CHAT_PATH))
        .respond_with(chat(delta))
        .mount(&direct)
        .await;
    let (mut client, task) = serve(&direct, Some(&gateway)).await;
    let events = chat_events(&mut client).await;
    assert!(
        matches!(events.as_slice(), [Event::Delta(text), Event::Usage(usage)]
        if text == delta && usage.provider == "openrouter"
            && usage.model == "tencent/hy4-flash" && !usage.fallback_used
            && usage.openrouter_transport.as_ref().is_some_and(|route| route.gateway_fallback
                && route.route() == transport::Route::Direct && route.gateway_reason() == transport::Reason::Unavailable
                && route.gateway_cache_status() == transport::CacheStatus::Bypass)
            && usage.output_tokens == 5)
    );
    let via_gateway = gateway.received_requests().await.unwrap();
    let via_direct = direct.received_requests().await.unwrap();
    assert_eq!(via_gateway.len(), 1);
    assert_eq!(via_direct.len(), 1);
    assert_eq!(via_gateway[0].body, via_direct[0].body);
    task.abort();
}

#[tokio::test]
async fn gateway_success_avoids_direct_and_transcription_uses_direct_by_default() {
    let gateway = MockServer::start().await;
    let direct = MockServer::start().await;
    Mock::given(path(CHAT_PATH))
        .respond_with(chat("gateway reply"))
        .mount(&gateway)
        .await;
    Mock::given(path("/api/v1/audio/transcriptions"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({
            "text": "synthetic transcript", "usage": {"seconds": 1.0, "cost": 0.00001}
        })))
        .mount(&direct)
        .await;
    let (mut client, task) = serve(&direct, Some(&gateway)).await;
    let events = chat_events(&mut client).await;
    assert!(
        matches!(events.as_slice(), [Event::Delta(text), Event::Usage(usage)]
        if text == "gateway reply" && usage.provider == "openrouter" && !usage.fallback_used)
    );
    assert!(direct.received_requests().await.unwrap().is_empty());
    let transcription = client
        .transcribe(pb::TranscribeRequest {
            audio: vec![1, 2, 3],
            container: "webm".into(),
            duration_ms: 1_000,
            purpose: "tutor".into(),
            ..Default::default()
        })
        .await
        .unwrap()
        .into_inner();
    assert_eq!(transcription.text, "synthetic transcript");
    assert_eq!(
        transcription.openrouter_transport.unwrap().route(),
        transport::Route::Direct
    );
    assert_eq!(direct.received_requests().await.unwrap().len(), 1);
    assert_eq!(gateway.received_requests().await.unwrap().len(), 1);
    task.abort();
}

#[tokio::test]
async fn gateway_error_after_delta_never_replays_stream_over_grpc() {
    let gateway = MockServer::start().await;
    let direct = MockServer::start().await;
    let case = gateway_case("stream_after_delta");
    let delta = case["gateway_delta"].as_str().unwrap();
    Mock::given(path(CHAT_PATH))
        .respond_with(ResponseTemplate::new(200).set_body_raw(
            format!(
                "data: {}\n\ndata: {}\n\n",
                json!({"choices":[{"delta":{"content":delta}}]}),
                case["gateway_error"]
            ),
            "text/event-stream",
        ))
        .mount(&gateway)
        .await;
    let (mut client, task) = serve(&direct, Some(&gateway)).await;
    let events = chat_events(&mut client).await;
    assert!(matches!(events.first(), Some(Event::Delta(text)) if text == delta));
    assert!(
        events
            .iter()
            .any(|event| matches!(event, Event::StreamError(_)))
    );
    assert!(direct.received_requests().await.unwrap().is_empty());
    assert_eq!(gateway.received_requests().await.unwrap().len(), 1);
    task.abort();
}

#[tokio::test]
async fn generation_defaults_to_direct_openrouter() {
    let direct = MockServer::start().await;
    Mock::given(path(DIRECT_CHAT_PATH))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({
            "choices": [{"message": {"content": json!({"summary": "A star note", "tags": [], "links": []}).to_string()}}],
            "usage": {"prompt_tokens": 20, "completion_tokens": 10}
        })))
        .mount(&direct)
        .await;
    let (mut client, task) = serve(&direct, None).await;
    let reply = client
        .enrich_note(pb::EnrichNoteRequest {
            note: "Stars".into(),
            ..Default::default()
        })
        .await
        .unwrap()
        .into_inner();
    assert_eq!(reply.summary, "A star note");
    assert_eq!(direct.received_requests().await.unwrap().len(), 1);
    task.abort();
}
