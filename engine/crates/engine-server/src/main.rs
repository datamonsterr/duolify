use engine_proto::engine_service_server::EngineServiceServer;
use engine_server::Service;
use std::sync::Arc;
use tonic::transport::Server;
#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt()
        .json()
        .with_env_filter("engine_server=info")
        .with_span_events(tracing_subscriber::fmt::format::FmtSpan::CLOSE)
        .init();

    // Release gate: validate engine startup config and private networking
    let startup_cfg = engine_core::backup::EngineStartupConfig::from_env();
    startup_cfg.validate()?;

    let addr = startup_cfg.listen_addr.parse()?;
    let mode = startup_cfg.provider_mode;
    let service = EngineServiceServer::new(Service {
        router: Arc::new(engine_providers::router(&mode)?),
    });
    let (reporter, health) = tonic_health::server::health_reporter();
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
