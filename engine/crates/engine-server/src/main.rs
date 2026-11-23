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
    let service = Service::new(
        Arc::new(engine_providers::router(&mode)?),
        engine_providers::elevenlabs::select_speech(|k| std::env::var(k).ok()),
    );
    // E10-06: one line per gated task, so a deploy without valid evidence is alertable.
    engine_server::log_promotion_status(&service);
    let service = EngineServiceServer::new(service);
    let (reporter, health) = tonic_health::server::health_reporter();
    reporter.set_serving::<EngineServiceServer<Service>>().await;
    let reflection = tonic_reflection::server::Builder::configure()
        .register_encoded_file_descriptor_set(engine_proto::FILE_DESCRIPTOR_SET)
        .build_v1()?;
    tracing::info!(%addr, "engine listening");
    // SIGTERM: report NOT_SERVING (health watchers and probes see the drain), stop accepting, let in-flight
    // RPCs finish for up to the drain timeout (a deploy replaces this container; stop_grace_period is longer).
    let drain = engine_server::drain_timeout_from(
        std::env::var("ENGINE_DRAIN_TIMEOUT_SECS").ok().as_deref(),
    );
    engine_server::serve_with_drain(
        |stop| {
            Server::builder()
                .add_service(health)
                .add_service(reflection)
                .add_service(service)
                .serve_with_shutdown(addr, stop)
        },
        async {
            shutdown().await;
            reporter
                .set_not_serving::<EngineServiceServer<Service>>()
                .await;
        },
        drain,
    )
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
