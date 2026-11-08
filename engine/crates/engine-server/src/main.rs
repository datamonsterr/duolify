use engine_proto::engine_service_server::EngineServiceServer;
use engine_server::Service;
use std::sync::Arc;
use tonic::transport::Server;
#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt()
        .json()
        .with_env_filter(tracing_subscriber::EnvFilter::from_default_env())
        .init();
    let addr = std::env::var("ENGINE_ADDR_LISTEN")
        .unwrap_or_else(|_| "0.0.0.0:50051".into())
        .parse()?;
    let mode = std::env::var("ENGINE_PROVIDER_MODE").unwrap_or_default();
    let service = EngineServiceServer::new(Service {
        router: Arc::new(engine_providers::router(&mode)?),
    });
    let (mut reporter, health) = tonic_health::server::health_reporter();
    reporter.set_serving::<EngineServiceServer<Service>>().await;
    let reflection = tonic_reflection::server::Builder::configure()
        .register_encoded_file_descriptor_set(engine_proto::FILE_DESCRIPTOR_SET)
        .build_v1()?;
    tracing::info!(%addr, "engine listening");
    Server::builder()
        .add_service(health)
        .add_service(reflection)
        .add_service(service)
        .serve_with_shutdown(addr, shutdown())
        .await?;
    Ok(())
}
async fn shutdown() {
    #[cfg(unix)]
    {
        let mut sigterm = tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
            .expect("SIGTERM handler");
        tokio::select! { _ = tokio::signal::ctrl_c() => {}, _ = sigterm.recv() => {} }
    }
    #[cfg(not(unix))]
    {
        let _ = tokio::signal::ctrl_c().await;
    }
}
