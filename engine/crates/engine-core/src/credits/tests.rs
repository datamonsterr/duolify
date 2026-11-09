use super::*;
#[test]
fn envelope_rejects_missing_and_excessive_bounds() {
    assert!(validate_limits(&Limits::default()).is_err());
    let limits = Limits {
        input_tokens: 20,
        output_tokens: 10,
        timeout_ms: 100,
        max_attempts: 2,
        max_cost_nanos: 1000,
        input_nanos: 10,
        output_nanos: 20,
        cache_write_nanos: 15,
        cache_read_nanos: 1,
        deadline_unix_ms: 0,
    };
    assert!(validate_limits(&limits).is_ok());
    assert!(check_usage(&limits, 21, 0, 0, 0).is_err());
    assert_eq!(check_usage(&limits, 2, 3, 4, 5).unwrap(), 145);
}

struct Slow;
#[async_trait::async_trait]
impl crate::LlmProvider for Slow {
    fn name(&self) -> &'static str {
        "slow"
    }
    async fn complete(&self, _model: &str, _input: &str) -> anyhow::Result<crate::Completion> {
        tokio::time::sleep(std::time::Duration::from_secs(1)).await;
        anyhow::bail!("timeout fixture")
    }
    async fn stream(&self, _model: &str, _input: &str) -> anyhow::Result<crate::CompletionStream> {
        anyhow::bail!("no streaming")
    }
}
#[tokio::test]
async fn timeout_cancels_before_fallback_and_retains_unknown_usage() {
    let provider = std::sync::Arc::new(Slow);
    let router = crate::ProviderRouter::new(provider.clone(), provider).unwrap();
    let limits = Limits {
        input_tokens: 20,
        output_tokens: 10,
        timeout_ms: 5,
        max_attempts: 2,
        max_cost_nanos: 1000,
        input_nanos: 10,
        output_nanos: 20,
        cache_write_nanos: 15,
        cache_read_nanos: 1,
        deadline_unix_ms: 0,
    };
    let outcome = run(
        &router,
        "preview",
        "fixture",
        &limits,
        crate::courses::validate_preview,
    )
    .await
    .unwrap();
    assert!(outcome.completion.is_none());
    assert_eq!(outcome.attempts.len(), 1);
    assert!(outcome.attempts[0].usage_unavailable);
}

struct UnknownUsage(bool);

#[tokio::test]
async fn reservation_deadline_cannot_restart_at_the_engine() {
    let provider = std::sync::Arc::new(Slow);
    let router = crate::ProviderRouter::new(provider.clone(), provider).unwrap();
    let base = Limits {
        input_tokens: 20,
        output_tokens: 10,
        timeout_ms: 1000,
        max_attempts: 2,
        max_cost_nanos: 1000,
        input_nanos: 10,
        output_nanos: 20,
        cache_write_nanos: 15,
        cache_read_nanos: 1,
        deadline_unix_ms: 0,
    };
    let mut value = serde_json::to_value(base).unwrap();
    value["deadline_unix_ms"] = 1.into();
    let limits = serde_json::from_value(value).unwrap();
    let outcome = run_validated(
        &router,
        "preview",
        "fixture",
        &limits,
        crate::courses::validate_preview,
    )
    .await
    .unwrap();
    assert!(
        outcome.attempts.is_empty(),
        "expired reservation called provider"
    );

    // The provider must also stop within a still-live reservation's remaining time.
    let mut live = limits;
    let now = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap()
        .as_millis() as u64;
    live.deadline_unix_ms = now + 20;
    let started = std::time::Instant::now();
    let outcome = run_validated(
        &router,
        "preview",
        "fixture",
        &live,
        crate::courses::validate_preview,
    )
    .await
    .unwrap();
    assert!(outcome.completion.is_none());
    assert!(
        started.elapsed() < std::time::Duration::from_millis(300),
        "engine restarted relative timeout"
    );
}
#[async_trait::async_trait]
impl crate::LlmProvider for UnknownUsage {
    fn name(&self) -> &'static str {
        "fixture"
    }
    async fn complete(&self, model: &str, _input: &str) -> anyhow::Result<crate::Completion> {
        Ok(serde_json::from_value(
            serde_json::json!({"text":"{}","provider":"fixture","model":model,"input_tokens":0,"output_tokens":0,"usage_unavailable":self.0}),
        )?)
    }
    async fn stream(&self, _: &str, _: &str) -> anyhow::Result<crate::CompletionStream> {
        anyhow::bail!("not streaming")
    }
}

#[tokio::test]
async fn missing_usage_retains_budget_while_measured_zero_allows_fallback() {
    let limits = Limits {
        input_tokens: 20,
        output_tokens: 10,
        timeout_ms: 100,
        max_attempts: 2,
        max_cost_nanos: 500,
        input_nanos: 10,
        output_nanos: 20,
        cache_write_nanos: 15,
        cache_read_nanos: 1,
        deadline_unix_ms: 0,
    };
    for unknown in [false, true] {
        let provider = std::sync::Arc::new(UnknownUsage(unknown));
        let router = crate::ProviderRouter::new(provider.clone(), provider).unwrap();
        let outcome = run_validated(&router, "preview", "fixture", &limits, |_| false)
            .await
            .unwrap();
        assert!(outcome.completion.is_none());
        assert_eq!(outcome.attempts[0].usage_unavailable, unknown);
        assert_eq!(outcome.attempts.len(), if unknown { 1 } else { 2 });
    }
}
