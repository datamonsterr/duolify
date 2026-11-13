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

#[tokio::test]
async fn an_approved_fixture_voice_resolves_to_its_own_voice_and_fingerprint() {
    let mut c = client().await;
    let default = c
        .synthesize_narration(request("ignored", true))
        .await
        .unwrap()
        .into_inner();
    let mut warm = request("ignored", true);
    warm.voice_key = "fixture-warm".into();
    let warm = c.synthesize_narration(warm).await.unwrap().into_inner();
    assert_eq!(warm.voice_id, "mock-fixture-warm-voice");
    assert_ne!(warm.settings_fingerprint, default.settings_fingerprint);
    let mut spoken = request("The sky is a map.", false);
    spoken.voice_key = "fixture-warm".into();
    let r = c.synthesize_narration(spoken).await.unwrap().into_inner();
    assert_eq!(r.voice_id, warm.voice_id);
    assert_eq!(r.settings_fingerprint, warm.settings_fingerprint);
    assert_eq!(r.alignment.unwrap().characters.len(), 17);
}

struct Down(
    std::sync::atomic::AtomicU32,
    fn() -> engine_core::narration::VendorFailure,
);
#[async_trait::async_trait]
impl engine_core::narration::SpeechProvider for Down {
    fn name(&self) -> &'static str {
        "down"
    }
    async fn synthesize(
        &self,
        _: &engine_core::narration::SpeechRequest,
    ) -> anyhow::Result<engine_core::narration::SpeechOutput> {
        self.0.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        Err((self.1)().into())
    }
}

async fn client_with(
    speech: Arc<dyn engine_core::narration::SpeechProvider>,
) -> EngineServiceClient<tonic::transport::Channel> {
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    let server = EngineServiceServer::new(Service {
        router: Arc::new(engine_providers::router("mock").unwrap()),
        speech,
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

#[tokio::test]
async fn vendor_failures_stay_inside_one_retry_and_map_to_fixed_statuses() {
    use engine_core::narration::VendorFailure;
    let cases: [(fn() -> VendorFailure, Code); 2] = [
        (|| VendorFailure::RateLimited, Code::ResourceExhausted),
        (
            || VendorFailure::Unavailable("status 503".into()),
            Code::Unavailable,
        ),
    ];
    for (failure, code) in cases {
        let down = Arc::new(Down(0.into(), failure));
        let mut c = client_with(down.clone()).await;
        let mut r = request("The sky is a map.", false);
        // The quote asks for more than the launch limits allow; the engine clamps it.
        r.limits = Some(pb::UsageLimits {
            model: "eleven_flash_v2_5".into(),
            timeout_ms: 120_000,
            max_attempts: 5,
            ..Default::default()
        });
        let err = c.synthesize_narration(r).await.unwrap_err();
        assert_eq!(err.code(), code);
        assert!(!err.message().contains("sky"));
        assert_eq!(down.0.load(std::sync::atomic::Ordering::SeqCst), 2);
    }
}
