use engine_proto::{
    self as pb, engine_service_client::EngineServiceClient,
    engine_service_server::EngineServiceServer,
};
use engine_server::Service;
use std::sync::Arc;
use tokio_stream::wrappers::TcpListenerStream;
use tonic::{Code, transport::Server};

async fn client() -> EngineServiceClient<tonic::transport::Channel> {
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    let server = EngineServiceServer::new(Service {
        router: Arc::new(engine_providers::router("mock").unwrap()),
        speech: Arc::new(engine_core::narration::MockSpeechProvider),
    });
    tokio::spawn(async move {
        Server::builder()
            .add_service(server)
            .serve_with_incoming(TcpListenerStream::new(listener))
            .await
            .unwrap();
    });
    EngineServiceClient::connect(format!("http://{addr}"))
        .await
        .unwrap()
}

fn request(text: &str, resolve_only: bool) -> pb::SynthesizeNarrationRequest {
    pb::SynthesizeNarrationRequest {
        slide_id: "sky-1".into(),
        text: text.into(),
        language: "en".into(),
        voice_key: "default".into(),
        limits: Some(pb::UsageLimits {
            model: "eleven_flash_v2_5".into(),
            timeout_ms: 5000,
            max_attempts: 2,
            ..Default::default()
        }),
        resolve_only,
    }
}

#[tokio::test]
async fn synthesizes_mock_audio_with_alignment_and_usage_over_grpc() {
    let mut c = client().await;
    let resolved = c
        .synthesize_narration(request("ignored", true))
        .await
        .unwrap()
        .into_inner();
    assert!(resolved.audio.is_empty());
    assert_eq!(resolved.voice_id, "mock-default-voice");
    assert!(resolved.settings_fingerprint.starts_with("fp1-"));

    let text = "The sky is a map.";
    let r = c
        .synthesize_narration(request(text, false))
        .await
        .unwrap()
        .into_inner();
    assert_eq!(r.settings_fingerprint, resolved.settings_fingerprint);
    assert_eq!(r.characters as usize, text.chars().count());
    assert_eq!(r.alignment.unwrap().characters.len(), text.chars().count());
    assert_eq!(r.audio_bytes as usize, r.audio.len());
    assert!(r.duration_ms > 0);
    let usage = r.usage.unwrap();
    assert_eq!(
        (usage.component.as_str(), usage.provider.as_str()),
        ("audio", "elevenlabs")
    );
    assert_eq!(usage.narration_characters, r.characters);
}

#[tokio::test]
async fn rejects_missing_text_unknown_voice_and_foreign_model() {
    let mut c = client().await;
    let err = c
        .synthesize_narration(request("   ", false))
        .await
        .unwrap_err();
    assert_eq!(err.code(), Code::InvalidArgument);
    let mut unknown = request("Hello", false);
    unknown.voice_key = "other".into();
    assert_eq!(
        c.synthesize_narration(unknown).await.unwrap_err().code(),
        Code::FailedPrecondition
    );
    let mut foreign = request("Hello", false);
    foreign.limits.as_mut().unwrap().model = "another-model".into();
    assert_eq!(
        c.synthesize_narration(foreign).await.unwrap_err().code(),
        Code::FailedPrecondition
    );
}
