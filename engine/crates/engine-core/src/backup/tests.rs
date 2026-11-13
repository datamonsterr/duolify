use super::{Correlation, EngineEnvironmentPlan, EngineServiceConfig, redact_secrets};

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
fn validates_engine_environment_boundary_cases_and_rejects_violations() {
    let make_valid_plan = |env: &str| EngineEnvironmentPlan {
        environment: env.to_string(),
        project_name: "duolify".to_string(),
        engine_service: EngineServiceConfig {
            listen_addr: "[::]:50051".to_string(),
            domains: vec![],
            env_vars: [
                ("ENGINE_PROVIDER_MODE".to_string(), "mock".to_string()),
                ("ANTHROPIC_API_KEY".to_string(), "preserve()".to_string()),
            ]
            .into_iter()
            .collect(),
        },
        has_database: true,
        has_redis: true,
        has_assets_bucket: true,
        has_backups_bucket: true,
    };

    // Valid environments dev, staging, prod
    for env in ["dev", "staging", "prod"] {
        let plan = make_valid_plan(env);
        let res = plan.validate();
        assert!(res.is_valid, "expected env {} to be valid", env);
        assert!(!res.live_apply_allowed);
        assert_eq!(res.hitl_gate_required, "E11-07");
        assert!(res.violations.is_empty());
    }

    // Rejects unknown environment
    let invalid_env = make_valid_plan("qa");
    let res = invalid_env.validate();
    assert!(!res.is_valid);
    assert!(res.violations.iter().any(|v| v.contains("environment")));

    // Rejects public engine domain
    let mut public_engine = make_valid_plan("dev");
    public_engine.engine_service.domains = vec!["public.engine.railway.app".to_string()];
    let res = public_engine.validate();
    assert!(!res.is_valid);
    assert!(
        res.violations
            .iter()
            .any(|v| v.contains("public domain") || v.contains("domains"))
    );

    // Rejects missing database
    let mut no_db = make_valid_plan("staging");
    no_db.has_database = false;
    let res = no_db.validate();
    assert!(!res.is_valid);
    assert!(res.violations.iter().any(|v| v.contains("database")));

    // Rejects missing redis
    let mut no_redis = make_valid_plan("prod");
    no_redis.has_redis = false;
    let res = no_redis.validate();
    assert!(!res.is_valid);
    assert!(res.violations.iter().any(|v| v.contains("redis")));

    // Rejects missing storage buckets
    let mut no_storage = make_valid_plan("dev");
    no_storage.has_assets_bucket = false;
    let res = no_storage.validate();
    assert!(!res.is_valid);
    assert!(
        res.violations
            .iter()
            .any(|v| v.contains("storage") || v.contains("bucket"))
    );

    // Rejects plaintext secrets in config
    let mut secret_config = make_valid_plan("prod");
    secret_config.engine_service.env_vars.insert(
        "ANTHROPIC_API_KEY".to_string(),
        "sk-ant-api03-abcdef1234567890".to_string(),
    );
    let res = secret_config.validate();
    assert!(!res.is_valid);
    assert!(
        res.violations
            .iter()
            .any(|v| v.contains("secret") || v.contains("plaintext"))
    );

    // Secret must be redacted in violations and plan summary
    for v in &res.violations {
        assert!(!v.contains("sk-ant-api03-abcdef1234567890"));
    }
    assert!(!res.plan_summary.contains("sk-ant-api03-abcdef1234567890"));
}

#[test]
fn redacts_secrets_in_text() {
    let input = "engine failed with key sk-ant-1234567890abcdef and token Bearer my-secret-token";
    let redacted = redact_secrets(input);
    assert!(!redacted.contains("sk-ant-1234567890abcdef"));
    assert!(!redacted.contains("my-secret-token"));
    assert!(redacted.contains("[REDACTED]"));
}
