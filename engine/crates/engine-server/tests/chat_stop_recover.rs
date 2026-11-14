//! Over real gRPC: typed idle error with a bounded estimate, silent-primary fallback, and cancellation.
use engine_core::{Completion, CompletionStream, LlmProvider, ProviderRouter, StreamEvent};
use engine_proto::{
    self as pb, chat_event::Event, engine_service_client::EngineServiceClient,
    engine_service_server::EngineServiceServer,
};
use engine_server::Service;
use futures::StreamExt;
use std::sync::{
    Arc,
    atomic::{AtomicBool, Ordering},
};
use tokio_stream::wrappers::TcpListenerStream;
use tonic::transport::Server;

struct Scripted {
    name: &'static str,
    deltas: Vec<&'static str>,
    hang: bool,
    dropped: Arc<AtomicBool>,
}
struct OnDrop(Arc<AtomicBool>);
impl Drop for OnDrop {
    fn drop(&mut self) {
        self.0.store(true, Ordering::SeqCst);
    }
}
#[tonic::async_trait]
impl LlmProvider for Scripted {
    fn name(&self) -> &'static str {
        self.name
    }
    async fn complete(&self, _: &str, _: &str) -> anyhow::Result<Completion> {
        anyhow::bail!("unused")
    }
    async fn stream(&self, _: &str, _: &str) -> anyhow::Result<CompletionStream> {
        let guard = OnDrop(self.dropped.clone());
        let items = futures::stream::iter(
            self.deltas
                .iter()
                .map(|d| Ok(StreamEvent::Delta((*d).into())))
                .collect::<Vec<_>>(),
        );
        let tail: CompletionStream = if self.hang {
            Box::pin(items.chain(futures::stream::pending()))
        } else {
            Box::pin(items)
        };
        Ok(Box::pin(tail.map(move |item| {
            let _keep = &guard;
            item
        })))
    }
}

async fn serve(
    primary: Scripted,
    fallback: Scripted,
) -> EngineServiceClient<tonic::transport::Channel> {
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    let service = EngineServiceServer::new(Service {
        router: Arc::new(ProviderRouter::new(Arc::new(primary), Arc::new(fallback)).unwrap()),
        speech: Arc::new(engine_core::narration::MockSpeechProvider),
    });
    tokio::spawn(async move {
        Server::builder()
            .add_service(service)
            .serve_with_incoming(TcpListenerStream::new(listener))
            .await
            .unwrap();
    });
    EngineServiceClient::connect(format!("http://{addr}"))
        .await
        .unwrap()
}
fn request(first_ms: u32, idle_ms: u32) -> pb::ChatRequest {
    pb::ChatRequest {
        messages: vec![pb::ChatMessage {
            role: "user".into(),
            content: "hello".into(),
        }],
        limits: Some(pb::UsageLimits {
            input_tokens: 4000,
            output_tokens: 8,
            timeout_ms: 30_000,
            max_attempts: 1,
            max_cost_nanos: 1_000_000_000,
            cache_write_nanos: 1,
            cache_read_nanos: 1,
            input_nanos: 1,
            output_nanos: 1,
            first_token_timeout_ms: first_ms,
            idle_timeout_ms: idle_ms,
            ..Default::default()
        }),
        ..Default::default()
    }
}
fn scripted(name: &'static str, deltas: Vec<&'static str>, hang: bool) -> Scripted {
    Scripted {
        name,
        deltas,
        hang,
        dropped: Arc::new(AtomicBool::new(false)),
    }
}

#[tokio::test]
async fn idle_timeout_is_typed_with_a_bounded_flagged_estimate_and_no_second_provider() {
    let fallback = scripted("fallback", vec!["SHOULD NOT APPEAR"], false);
    let mut client = serve(
        scripted(
            "primary",
            vec!["Moons ", "orbit planets far away beyond the sky"],
            true,
        ),
        fallback,
    )
    .await;
    let mut stream = client.chat(request(500, 60)).await.unwrap().into_inner();
    let mut text = String::new();
    let (mut typed, mut usage) = (None, None);
    while let Some(event) = stream.message().await.unwrap() {
        match event.event.unwrap() {
            Event::Delta(d) => text.push_str(&d),
            Event::StreamError(e) => typed = Some(e),
            Event::Usage(u) => usage = Some(u),
            other => panic!("unexpected {other:?}"),
        }
    }
    assert_eq!(text, "Moons orbit planets far away beyond the sky");
    let typed = typed.expect("typed error");
    assert_eq!(
        (typed.code.as_str(), typed.retryable),
        ("idle_timeout", true)
    );
    let usage = usage.expect("usage frame");
    assert!(usage.estimated);
    assert_eq!(usage.output_tokens, 8, "bounded by the quote's output cap");
    assert_eq!(usage.provider, "primary");
}

#[tokio::test]
async fn silent_primary_falls_back_once_and_reports_the_fallback_route() {
    let mut client = serve(
        scripted("primary", vec![], true),
        scripted("fallback", vec!["Hello"], false),
    )
    .await;
    let mut stream = client.chat(request(60, 500)).await.unwrap().into_inner();
    let mut text = String::new();
    let mut usage = None;
    while let Some(event) = stream.message().await.unwrap() {
        match event.event.unwrap() {
            Event::Delta(d) => text.push_str(&d),
            Event::Usage(u) => usage = Some(u),
            other => panic!("unexpected {other:?}"),
        }
    }
    assert_eq!(text, "Hello");
    let usage = usage.expect("estimate for a provider that sent no usage frame");
    assert!(usage.estimated);
    assert_eq!(usage.provider, "fallback");
}

#[tokio::test]
async fn silent_everywhere_is_a_typed_first_token_error_with_no_delivered_content() {
    let mut client = serve(
        scripted("primary", vec![], true),
        scripted("fallback", vec![], true),
    )
    .await;
    let mut stream = client.chat(request(40, 500)).await.unwrap().into_inner();
    let mut typed = None;
    let mut deltas = 0;
    while let Some(event) = stream.message().await.unwrap() {
        match event.event.unwrap() {
            Event::Delta(_) => deltas += 1,
            Event::StreamError(e) => typed = Some(e),
            Event::Usage(u) => assert_eq!(u.output_tokens, 0),
            other => panic!("unexpected {other:?}"),
        }
    }
    assert_eq!(deltas, 0);
    assert_eq!(typed.unwrap().code, "first_token_timeout");
}

#[tokio::test]
async fn cancelling_the_call_drops_the_provider_stream() {
    let primary = scripted("primary", vec!["partial"], true);
    let dropped = primary.dropped.clone();
    let mut client = serve(primary, scripted("fallback", vec![], false)).await;
    let mut stream = client
        .chat(request(500, 10_000))
        .await
        .unwrap()
        .into_inner();
    assert!(matches!(
        stream.message().await.unwrap().unwrap().event,
        Some(Event::Delta(_))
    ));
    drop(stream);
    for _ in 0..100 {
        if dropped.load(Ordering::SeqCst) {
            return;
        }
        tokio::time::sleep(std::time::Duration::from_millis(20)).await;
    }
    panic!("provider stream still running after the caller cancelled");
}
