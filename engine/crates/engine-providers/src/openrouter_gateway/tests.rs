use crate::openrouter_gateway::{GatewayConfig, classify_response};
use engine_core::GatewayReason;
use reqwest::StatusCode;
use serde_json::json;

#[test]
fn unset_and_explicit_default_use_direct_openrouter() {
    for url in [None, Some(""), Some("https://openrouter.ai/api/v1/")] {
        assert!(GatewayConfig::parse(url, None, None).unwrap().is_none());
    }
}

#[test]
fn joins_gateway_endpoints_and_rejects_invalid_startup_configuration() {
    let config = GatewayConfig::parse(
        Some("https://gateway.ai.cloudflare.com/v1/a/g/openrouter///"),
        Some("token"),
        Some("true"),
    )
    .unwrap()
    .unwrap();
    assert_eq!(
        config.url("chat/completions").as_str(),
        "https://gateway.ai.cloudflare.com/v1/a/g/openrouter/chat/completions"
    );
    assert_eq!(
        config.url("audio/transcriptions").as_str(),
        "https://gateway.ai.cloudflare.com/v1/a/g/openrouter/audio/transcriptions"
    );
    for value in [
        "definitely not a URL",
        "ftp://gateway.ai.cloudflare.com/v1/a/g/openrouter",
        "http://gateway.ai.cloudflare.com/v1/a/g/openrouter",
        "https://user:secret@gateway.ai.cloudflare.com/v1/a/g/openrouter",
        "https://gateway.ai.cloudflare.com/v1/a/g/openrouter?key=secret",
        "https://gateway.ai.cloudflare.com/v1/a/g/openrouter#fragment",
    ] {
        let error = GatewayConfig::parse(Some(value), None, None).err().unwrap();
        assert!(error.to_string().contains("OPENROUTER_BASE_URL"));
        assert!(!error.to_string().contains("secret"));
    }
    assert!(GatewayConfig::parse(None, Some("token"), None).is_err());
    assert!(GatewayConfig::parse(None, None, Some("true")).is_err());
    assert!(GatewayConfig::parse(None, None, Some("maybe")).is_err());
}

#[test]
fn only_unavailable_gateway_errors_trigger_a_direct_retry() {
    let cases = [
        (502, json!(null), Some(GatewayReason::Unavailable)),
        (503, json!(null), Some(GatewayReason::Unavailable)),
        (504, json!(null), Some(GatewayReason::Timeout)),
        (
            400,
            json!({"errors": [{"code": 2005}]}),
            Some(GatewayReason::Unavailable),
        ),
        (
            400,
            json!({"error": {"code": 2005}}),
            Some(GatewayReason::Unavailable),
        ),
        (
            200,
            json!({"error": {"code": 2005}}),
            Some(GatewayReason::Unavailable),
        ),
        (
            400,
            json!({"error": {"code": 400, "message": "request says 2005"}}),
            None,
        ),
        (400, json!({"errors": [{"code": 7003}]}), None),
        (401, json!({"errors": [{"code": 2009}]}), None),
        (403, json!({"errors": [{"code": 2049}]}), None),
        (424, json!({"errors": [{"code": 2016}]}), None),
        (429, json!({"errors": [{"code": 2003}]}), None),
        (302, json!(null), None),
    ];
    for (status, body, expected) in cases {
        assert_eq!(
            classify_response(StatusCode::from_u16(status).unwrap(), &body),
            expected
        );
    }
}
