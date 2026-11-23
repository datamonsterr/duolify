//! `Transcribe` RPC: mock mode over gRPC, typed refusals before any provider call, the promotion
//! gate on a live route, the audio request an OpenRouter double receives, and logs free of audio
//! bytes and transcript text.
use engine_core::{LlmProvider, ProviderRouter, promotion::Gate};
use engine_proto::{self as pb, engine_service_server::EngineService};
use engine_server::Service;
use serde_json::{Value, json};
use std::{
    io::Write,
    sync::{Arc, Mutex},
};
use tonic::{Code, Request};
use tonic_types::StatusExt;
use wiremock::{Mock, MockServer, ResponseTemplate, matchers::method};

fn mock_service() -> Service {
    Service {
        router: Arc::new(engine_providers::router("mock").unwrap()),
        speech: Arc::new(engine_core::narration::MockSpeechProvider),
        promotion: Default::default(),
    }
}

fn request(audio: Vec<u8>) -> Request<pb::TranscribeRequest> {
    Request::new(pb::TranscribeRequest {
        audio,
        container: "webm".into(),
        duration_ms: 5000,
        purpose: "tutor".into(),
        language_hint: String::new(),
    })
}

fn reason(status: &tonic::Status) -> String {
    status
        .get_details_error_info()
        .map(|i| i.reason)
        .unwrap_or_default()
}

async fn live(gate: Gate, answer: ResponseTemplate) -> (Service, MockServer) {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .respond_with(answer)
        .mount(&server)
        .await;
    let p: Arc<dyn LlmProvider> = Arc::new(engine_providers::HttpProvider::openrouter(
        "test-key".into(),
        format!("{}/api/v1/chat/completions", server.uri()),
    ));
    let svc = Service {
        router: Arc::new(ProviderRouter::new(p.clone(), p).unwrap()),
        speech: Arc::new(engine_core::narration::MockSpeechProvider),
        promotion: Arc::new(gate),
    };
    (svc, server)
}

fn heard(text: &str) -> ResponseTemplate {
    ResponseTemplate::new(200).set_body_json(json!({
        "text": text, "language": "vi", "usage": {"seconds": 4.5, "cost": 0.00001}
    }))
}

#[tokio::test]
async fn mock_mode_transcribes_over_the_service() {
    let r = mock_service()
        .transcribe(request(vec![1, 2, 3]))
        .await
        .unwrap()
        .into_inner();
    assert_eq!(r.text, "mock transcript");
    assert_eq!(r.model, "qwen/qwen3-asr-0.6b");
    assert!(!r.fallback_used);
    assert_eq!(r.cost_nanos, 0);
    let silence = mock_service()
        .transcribe(request(vec![0; 64]))
        .await
        .unwrap()
        .into_inner();
    assert_eq!(silence.text, "");
}

#[tokio::test]
async fn bad_recordings_are_refused_with_a_typed_reason() {
    let svc = mock_service();
    let bad = |mut r: pb::TranscribeRequest| {
        r.purpose = "tutor".into();
        Request::new(r)
    };
    let cases = [
        pb::TranscribeRequest {
            audio: vec![],
            container: "webm".into(),
            duration_ms: 5000,
            ..Default::default()
        },
        pb::TranscribeRequest {
            audio: vec![1; 1_000_001],
            container: "webm".into(),
            duration_ms: 5000,
            ..Default::default()
        },
        pb::TranscribeRequest {
            audio: vec![1],
            container: "webm".into(),
            duration_ms: 30_001,
            ..Default::default()
        },
        pb::TranscribeRequest {
            audio: vec![1],
            container: "mp3".into(),
            duration_ms: 5000,
            ..Default::default()
        },
    ];
    for case in cases {
        let status = svc.transcribe(bad(case)).await.unwrap_err();
        assert_eq!(status.code(), Code::InvalidArgument);
        assert_eq!(reason(&status), "invalid_audio");
    }
}

#[tokio::test]
async fn a_live_route_without_evidence_makes_no_provider_call() {
    for (gate, code) in [
        (Gate::from_report("{}", "fp"), "promotion_evidence_missing"),
        (
            Gate::for_tasks(&["tutor_chat"]),
            "promotion_task_not_passed",
        ),
    ] {
        let (svc, server) = live(gate, heard("never")).await;
        let status = svc.transcribe(request(vec![1; 10])).await.unwrap_err();
        assert_eq!(status.code(), Code::Unavailable);
        assert_eq!(reason(&status), code, "{}", status.message());
        assert!(server.received_requests().await.unwrap().is_empty());
    }
}

#[tokio::test]
async fn a_promoted_live_route_sends_private_audio_and_returns_usage() {
    let (svc, server) = live(Gate::for_tasks(&["transcribe"]), heard("xin chào")).await;
    let r = svc
        .transcribe(request(vec![9; 100]))
        .await
        .unwrap()
        .into_inner();
    assert_eq!(r.text, "xin chào");
    assert_eq!(r.language, "vi");
    assert_eq!(r.duration_ms, 4500);
    assert_eq!(r.cost_nanos, 10_000);
    assert_eq!(r.model, "qwen/qwen3-asr-0.6b");
    let sent: Value = server.received_requests().await.unwrap()[0]
        .body_json()
        .unwrap();
    assert_eq!(
        sent["provider"],
        json!({"data_collection": "deny", "zdr": true})
    );
    assert_eq!(sent["input_audio"]["format"], "webm");
}

#[tokio::test]
async fn provider_failure_and_overspend_are_typed() {
    let (svc, server) = live(Gate::for_tasks(&["transcribe"]), ResponseTemplate::new(500)).await;
    let status = svc.transcribe(request(vec![1; 10])).await.unwrap_err();
    assert_eq!(
        (status.code(), reason(&status).as_str()),
        (Code::Unavailable, "transcribe_failed")
    );
    assert_eq!(
        server.received_requests().await.unwrap().len(),
        2,
        "primary then fallback once"
    );

    let dear = ResponseTemplate::new(200)
        .set_body_json(json!({"text": "x", "usage": {"seconds": 5.0, "cost": 0.001}}));
    let (svc, server) = live(Gate::for_tasks(&["transcribe"]), dear).await;
    let status = svc.transcribe(request(vec![1; 10])).await.unwrap_err();
    assert_eq!(reason(&status), "transcribe_cost_ceiling");
    assert_eq!(server.received_requests().await.unwrap().len(), 1);
}

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

#[tokio::test(flavor = "current_thread")]
async fn logs_hold_no_audio_and_no_transcript() {
    let sink = Collector(Arc::default());
    let writer = sink.clone();
    let subscriber = tracing_subscriber::fmt()
        .with_writer(move || writer.clone())
        .with_max_level(tracing::Level::TRACE)
        .finish();
    tracing::subscriber::set_global_default(subscriber).unwrap();
    let (svc, _server) = live(
        Gate::for_tasks(&["transcribe"]),
        heard("top-secret-sentence"),
    )
    .await;
    let audio: Vec<u8> = b"AUDIO-MARKER-BYTES".to_vec();
    svc.transcribe(request(audio)).await.unwrap();
    let _ = svc.transcribe(request(vec![])).await;
    let logs = String::from_utf8_lossy(&sink.0.lock().unwrap()).to_string();
    assert!(logs.contains("transcription finished"), "{logs}");
    for forbidden in ["top-secret-sentence", "AUDIO-MARKER", "QVVESU8", "test-key"] {
        assert!(
            !logs.contains(forbidden),
            "{forbidden} leaked into logs: {logs}"
        );
    }
}
