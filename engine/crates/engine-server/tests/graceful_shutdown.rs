use engine_server::{drain_timeout_from, serve_with_drain};
use std::time::{Duration, Instant};
use tokio_stream::{StreamExt, wrappers::TcpListenerStream};
use tonic::transport::Server;
use tonic_health::pb::{
    HealthCheckRequest, health_check_response::ServingStatus, health_client::HealthClient,
};

/// Starts a server whose only RPC of interest is the long-lived health Watch stream (an in-flight RPC).
async fn start(
    drain: Duration,
) -> (
    std::net::SocketAddr,
    tokio::sync::oneshot::Sender<()>,
    tokio::task::JoinHandle<Instant>,
) {
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    let (reporter, health) = tonic_health::server::health_reporter();
    let (sigterm, signal) = tokio::sync::oneshot::channel::<()>();
    let task = tokio::spawn(async move {
        serve_with_drain(
            |stop| {
                Server::builder()
                    .add_service(health)
                    .serve_with_incoming_shutdown(TcpListenerStream::new(listener), stop)
            },
            async move {
                let _ = signal.await;
                // Same order as main: the service reports NOT_SERVING while draining.
                reporter
                    .set_service_status("", tonic_health::ServingStatus::NotServing)
                    .await;
            },
            drain,
        )
        .await
        .unwrap();
        Instant::now()
    });
    (addr, sigterm, task)
}

async fn try_client(
    addr: std::net::SocketAddr,
) -> Result<HealthClient<tonic::transport::Channel>, tonic::transport::Error> {
    let channel = tonic::transport::Endpoint::from_shared(format!("http://{addr}"))
        .unwrap()
        .connect()
        .await?;
    Ok(HealthClient::new(channel))
}

async fn client(addr: std::net::SocketAddr) -> HealthClient<tonic::transport::Channel> {
    try_client(addr).await.unwrap()
}

#[tokio::test]
async fn in_flight_rpc_finishes_then_server_exits_before_the_drain_timeout() {
    let (addr, sigterm, task) = start(Duration::from_secs(30)).await;
    let mut c = client(addr).await;
    let mut watch = c
        .watch(HealthCheckRequest {
            service: String::new(),
        })
        .await
        .unwrap()
        .into_inner();
    assert_eq!(
        watch.next().await.unwrap().unwrap().status,
        ServingStatus::Serving as i32
    );
    let started = Instant::now();
    sigterm.send(()).unwrap();
    // The in-flight stream is still served: it reports the drain, then the client finishes with it.
    assert_eq!(
        watch.next().await.unwrap().unwrap().status,
        ServingStatus::NotServing as i32
    );
    drop(watch);
    drop(c);
    let ended = tokio::time::timeout(Duration::from_secs(10), task)
        .await
        .unwrap()
        .unwrap();
    assert!(
        ended.duration_since(started) < Duration::from_secs(10),
        "exit waited for the full drain timeout"
    );
    // No new connections once draining.
    assert!(try_client(addr).await.is_err());
}

#[tokio::test]
async fn stuck_rpc_is_cut_at_the_drain_timeout() {
    let (addr, sigterm, task) = start(Duration::from_millis(500)).await;
    let mut c = client(addr).await;
    let _watch = c
        .watch(HealthCheckRequest {
            service: String::new(),
        })
        .await
        .unwrap()
        .into_inner();
    let started = Instant::now();
    sigterm.send(()).unwrap();
    let ended = tokio::time::timeout(Duration::from_secs(10), task)
        .await
        .unwrap()
        .unwrap();
    let took = ended.duration_since(started);
    assert!(
        took >= Duration::from_millis(450) && took < Duration::from_secs(5),
        "took {took:?}"
    );
}

#[test]
fn drain_timeout_parsing() {
    assert_eq!(
        drain_timeout_from(None),
        engine_server::DEFAULT_DRAIN_TIMEOUT
    );
    assert_eq!(
        drain_timeout_from(Some("0")),
        engine_server::DEFAULT_DRAIN_TIMEOUT
    );
    assert_eq!(
        drain_timeout_from(Some("x")),
        engine_server::DEFAULT_DRAIN_TIMEOUT
    );
    assert_eq!(drain_timeout_from(Some("45")), Duration::from_secs(45));
}
