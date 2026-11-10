use anyhow::{Context, Result, bail};
use serde_json::json;

pub const SYSTEM_OPERATIONAL_UUID: &str = "00000000-0000-4000-8000-000000000011";

pub fn validate_structural_privacy(properties: &serde_json::Value) -> Result<()> {
    const ALLOWLISTED_KEYS: &[&str] = &[
        "task",
        "score",
        "threshold",
        "passed",
        "dataset_revision",
        "dataset_source",
        "provider",
        "status",
        "budget_usd",
        "budget",
        "source",
        "platform",
        "app_version",
    ];

    let obj = properties
        .as_object()
        .context("properties must be a json object")?;
    for (k, v) in obj {
        if !ALLOWLISTED_KEYS.contains(&k.as_str()) {
            bail!(
                "structural privacy violation: unregistered property key '{k}' in eval_run_completed"
            );
        }
        match v {
            serde_json::Value::String(s) => {
                if s.len() > 100 || s.contains('\n') || s.contains('\r') {
                    bail!(
                        "structural privacy violation: string value for '{k}' exceeds length/format limits"
                    );
                }
            }
            serde_json::Value::Number(_) | serde_json::Value::Bool(_) | serde_json::Value::Null => {
            }
            _ => {
                bail!("structural privacy violation: complex type for '{k}' not permitted");
            }
        }
    }
    Ok(())
}

#[allow(clippy::too_many_arguments)]
pub async fn emit_eval_run_completed(
    host: &str,
    api_key: &str,
    task: &str,
    score: f64,
    threshold: f64,
    passed: bool,
    dataset_revision: &str,
    dataset_source: &str,
    provider: &str,
    status: &str,
    budget_usd: Option<f64>,
) -> Result<bool> {
    let opt_out = std::env::var("ANALYTICS_OPT_OUT")
        .map(|v| v.eq_ignore_ascii_case("true"))
        .unwrap_or(false);

    if opt_out {
        return Ok(false);
    }

    if api_key.is_empty() || host.is_empty() {
        return Ok(false);
    }

    let app_version = std::env::var("APP_VERSION").unwrap_or_else(|_| "development".to_string());

    let mut properties = json!({
        "task": task,
        "score": score,
        "threshold": threshold,
        "passed": passed,
        "dataset_revision": dataset_revision,
        "dataset_source": dataset_source,
        "provider": provider,
        "status": status,
        "source": "ci",
        "platform": "web",
        "app_version": app_version
    });

    if let Some(b) = budget_usd {
        properties["budget_usd"] = json!(b);
        properties["budget"] = json!(b);
    }

    // Structural privacy guard: allowlisted keys only
    validate_structural_privacy(&properties)?;

    let batch = json!({
        "api_key": api_key,
        "batch": [{
            "distinct_id": SYSTEM_OPERATIONAL_UUID,
            "event": "eval_run_completed",
            "properties": properties
        }]
    });

    let endpoint = format!("{}/batch", host.trim_end_matches('/'));
    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(10))
        .build()?;
    let res = client.post(&endpoint).json(&batch).send().await?;

    if !res.status().is_success() {
        bail!("failed to emit eval_run_completed: status {}", res.status());
    }

    Ok(true)
}

#[cfg(test)]
mod tests {
    use super::*;
    use wiremock::{
        Mock, MockServer, ResponseTemplate,
        matchers::{method, path},
    };

    #[test]
    fn structural_privacy_guard_enforces_allowlisted_keys_only() {
        let valid = json!({
            "task": "course_preview",
            "score": 1.0,
            "threshold": 1.0,
            "passed": true,
            "dataset_revision": "0123456789abcdef0123456789abcdef01234567",
            "dataset_source": "mirror",
            "provider": "live",
            "status": "passed",
            "budget_usd": 5.0,
            "source": "ci",
            "platform": "web",
            "app_version": "development"
        });
        assert!(validate_structural_privacy(&valid).is_ok());

        let unallowlisted_key = json!({
            "task": "course_preview",
            "prompt": "some prompt text",
            "score": 1.0,
            "threshold": 1.0,
            "passed": true
        });
        assert!(validate_structural_privacy(&unallowlisted_key).is_err());

        let nested_learner_data = json!({
            "task": "course_preview",
            "score": 1.0,
            "threshold": 1.0,
            "passed": true,
            "learner_profile": {"topic": "Coffee"}
        });
        assert!(validate_structural_privacy(&nested_learner_data).is_err());
    }

    #[tokio::test]
    async fn eval_run_completed_emitter_delivers_to_posthog_double_and_honors_opt_out() {
        let mock_server = MockServer::start().await;

        Mock::given(method("POST"))
            .and(path("/batch"))
            .respond_with(ResponseTemplate::new(200))
            .expect(1)
            .mount(&mock_server)
            .await;

        let sent = emit_eval_run_completed(
            &mock_server.uri(),
            "test-key",
            "course_preview",
            1.0,
            1.0,
            true,
            "0123456789abcdef0123456789abcdef01234567",
            "mirror",
            "live",
            "passed",
            Some(5.0),
        )
        .await
        .unwrap();

        assert!(sent);

        // Verify request received by double
        let received = mock_server.received_requests().await.unwrap();
        assert_eq!(received.len(), 1);
        let body: serde_json::Value = serde_json::from_slice(&received[0].body).unwrap();
        let event = &body["batch"][0];
        assert_eq!(event["distinct_id"], SYSTEM_OPERATIONAL_UUID);
        assert_eq!(event["event"], "eval_run_completed");
        assert_eq!(event["properties"]["task"], "course_preview");
        assert_eq!(event["properties"]["score"], 1.0);
        assert_eq!(event["properties"]["threshold"], 1.0);
        assert_eq!(event["properties"]["passed"], true);
        assert_eq!(
            event["properties"]["dataset_revision"],
            "0123456789abcdef0123456789abcdef01234567"
        );
        assert_eq!(event["properties"]["dataset_source"], "mirror");
        assert_eq!(event["properties"]["provider"], "live");
        assert_eq!(event["properties"]["status"], "passed");
        assert_eq!(event["properties"]["budget_usd"], 5.0);
        assert_eq!(event["properties"]["source"], "ci");
        assert_eq!(event["properties"]["platform"], "web");
        assert_eq!(event["properties"]["app_version"], "development");

        // Opt out test: must send zero requests
        unsafe { std::env::set_var("ANALYTICS_OPT_OUT", "true") };
        let sent_opted_out = emit_eval_run_completed(
            &mock_server.uri(),
            "test-key",
            "course_preview",
            1.0,
            1.0,
            true,
            "0123456789abcdef0123456789abcdef01234567",
            "mirror",
            "live",
            "passed",
            Some(5.0),
        )
        .await
        .unwrap();
        assert!(!sent_opted_out);
        assert_eq!(mock_server.received_requests().await.unwrap().len(), 1);
        unsafe { std::env::remove_var("ANALYTICS_OPT_OUT") };
    }
}
