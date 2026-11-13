use super::{Correlation, EngineConfigError, EngineStartupConfig, redact_secrets};

#[test]
fn validates_correlation_without_retaining_private_metadata() {
    let traceparent = "00-11111111111111111111111111111111-2222222222222222-01";
    let value = Correlation::parse("ABCDEFGHIJKLMNOPQRSTUVWXYZ", traceparent).unwrap();
    assert_eq!(value.request_id, "ABCDEFGHIJKLMNOPQRSTUVWXYZ");
    assert_eq!(value.trace_id, "11111111111111111111111111111111");
    assert_eq!(value.parent_span_id, "2222222222222222");
    for id in [
        "Bearer private-token",
        "raw prompt",
        "private\ntext",
        "",
        &"x".repeat(200),
    ] {
        assert!(Correlation::parse(id, traceparent).is_none());
    }
    for parent in [
        "private-text",
        "00-00000000000000000000000000000000-2222222222222222-01",
        "00-11111111111111111111111111111111-0000000000000000-01",
        "00-11111111111111111111111111111111-2222222222222222-zz",
    ] {
        assert!(Correlation::parse("ABCDEFGHIJKLMNOPQRSTUVWXYZ", parent).is_none());
    }
}

#[test]
fn engine_startup_config_validates_environments_and_enforces_release_gates() {
    // 1. Development: mock mode, no keys needed, private bind -> Ok
    let dev_config = EngineStartupConfig {
        duolify_env: "development".to_string(),
        listen_addr: "0.0.0.0:50051".to_string(),
        provider_mode: "mock".to_string(),
        public_domain: None,
        anthropic_api_key: None,
        openrouter_api_key: None,
    };
    assert!(dev_config.validate().is_ok());

    // 2. Staging: mock mode, private bind, no public domain -> Ok
    let staging_config = EngineStartupConfig {
        duolify_env: "staging".to_string(),
        listen_addr: "[::]:50051".to_string(),
        provider_mode: "mock".to_string(),
        public_domain: None,
        anthropic_api_key: None,
        openrouter_api_key: None,
    };
    assert!(staging_config.validate().is_ok());

    // 3. Production: live mode, with provider API key -> Ok
    let prod_config = EngineStartupConfig {
        duolify_env: "production".to_string(),
        listen_addr: "[::]:50051".to_string(),
        provider_mode: "live".to_string(),
        public_domain: None,
        anthropic_api_key: Some("sk-ant-test-key-12345".to_string()),
        openrouter_api_key: None,
    };
    assert!(prod_config.validate().is_ok());

    // 4. Staging refuses to start if exposed publicly via domain
    let staging_exposed = EngineStartupConfig {
        duolify_env: "staging".to_string(),
        listen_addr: "0.0.0.0:50051".to_string(),
        provider_mode: "mock".to_string(),
        public_domain: Some("engine-staging.railway.app".to_string()),
        anthropic_api_key: None,
        openrouter_api_key: None,
    };
    match staging_exposed.validate() {
        Err(EngineConfigError::PublicExposure(msg)) => {
            assert!(msg.contains("never have public domain"));
        }
        other => panic!("expected PublicExposure error, got {:?}", other),
    }

    // 5. Production refuses to start if exposed publicly via domain
    let prod_exposed = EngineStartupConfig {
        duolify_env: "production".to_string(),
        listen_addr: "0.0.0.0:50051".to_string(),
        provider_mode: "live".to_string(),
        public_domain: Some("engine.duolify.com".to_string()),
        anthropic_api_key: Some("sk-ant-test-key-12345".to_string()),
        openrouter_api_key: None,
    };
    match prod_exposed.validate() {
        Err(EngineConfigError::PublicExposure(msg)) => {
            assert!(msg.contains("engine.duolify.com"));
        }
        other => panic!("expected PublicExposure error, got {:?}", other),
    }

    // 6. Production live mode refuses to start without LLM credentials
    let prod_missing_keys = EngineStartupConfig {
        duolify_env: "production".to_string(),
        listen_addr: "0.0.0.0:50051".to_string(),
        provider_mode: "live".to_string(),
        public_domain: None,
        anthropic_api_key: None,
        openrouter_api_key: None,
    };
    match prod_missing_keys.validate() {
        Err(EngineConfigError::MissingDependency(msg)) => {
            assert!(msg.contains("ANTHROPIC_API_KEY") || msg.contains("OPENROUTER_API_KEY"));
        }
        other => panic!("expected MissingDependency error, got {:?}", other),
    }

    // 7. Invalid listen address fails validation
    let invalid_addr = EngineStartupConfig {
        duolify_env: "staging".to_string(),
        listen_addr: "not-an-ip-or-port".to_string(),
        provider_mode: "mock".to_string(),
        public_domain: None,
        anthropic_api_key: None,
        openrouter_api_key: None,
    };
    match invalid_addr.validate() {
        Err(EngineConfigError::InvalidAddress(msg)) => {
            assert!(msg.contains("not-an-ip-or-port"));
        }
        other => panic!("expected InvalidAddress error, got {:?}", other),
    }
}

#[test]
fn engine_redacts_sensitive_credentials_in_logs_and_errors() {
    let raw = "Config dump: anthropic=sk-ant-secret123, openrouter=sk-or-token456, auth=Bearer my-token, jwt=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.doNotLeakThis, db=postgres://user:pass123@postgres:5432/db";
    let redacted = redact_secrets(raw);
    assert!(!redacted.contains("sk-ant-secret123"));
    assert!(!redacted.contains("sk-or-token456"));
    assert!(!redacted.contains("my-token"));
    assert!(!redacted.contains("doNotLeakThis"));
    assert!(!redacted.contains("pass123"));
    assert!(redacted.contains("[REDACTED]"));
}
