use anyhow::{Result, bail};
use serde_json::json;

pub const SYSTEM_OPERATIONAL_UUID: &str = "00000000-0000-4000-8000-000000000011";

pub async fn emit_eval_run_completed(
    host: &str,
    api_key: &str,
    task: &str,
    score: f64,
    threshold: f64,
    passed: bool,
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

    let properties = json!({
        "task": task,
        "score": score,
        "threshold": threshold,
        "passed": passed,
        "source": "ci",
        "platform": "web",
        "app_version": app_version
    });

    // Verify privacy: no raw text or unexpected fields
    let raw = properties.to_string();
    if raw.contains("private") || raw.contains("prompt") || raw.contains("learner") {
        bail!("privacy assertion failed: private content in eval analytics");
    }

    let batch = json!({
        "api_key": api_key,
        "batch": [{
            "distinct_id": SYSTEM_OPERATIONAL_UUID,
            "event": "eval_run_completed",
            "properties": properties
        }]
    });

    let endpoint = format!("{}/batch", host.trim_end_matches('/'));
    let client = reqwest::Client::new();
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
        )
        .await
        .unwrap();
        assert!(!sent_opted_out);
        assert_eq!(mock_server.received_requests().await.unwrap().len(), 1);
        unsafe { std::env::remove_var("ANALYTICS_OPT_OUT") };
    }
}
