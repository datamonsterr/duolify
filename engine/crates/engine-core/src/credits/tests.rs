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
    };
    let outcome = run(&router, "preview", "fixture", &limits).await.unwrap();
    assert!(outcome.completion.is_none());
    assert_eq!(outcome.attempts.len(), 1);
    assert!(outcome.attempts[0].usage_unavailable);
}
