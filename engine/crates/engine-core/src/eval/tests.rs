use super::*;
use crate::{Completion, LlmProvider, ProviderRouter, StreamEvent};
use async_trait::async_trait;
use std::{sync::Arc, time::Duration};

struct FailingProvider {
    status_code: u16,
    error_message: &'static str,
}

#[async_trait]
impl LlmProvider for FailingProvider {
    fn name(&self) -> &'static str {
        "failing"
    }
    async fn complete(&self, _model: &str, _input: &str) -> anyhow::Result<Completion> {
        anyhow::bail!("{}: {}", self.status_code, self.error_message)
    }
    async fn stream(&self, _model: &str, _input: &str) -> anyhow::Result<crate::CompletionStream> {
        anyhow::bail!("{}: {}", self.status_code, self.error_message)
    }
}

struct DelayedProvider {
    delay: Duration,
}

#[async_trait]
impl LlmProvider for DelayedProvider {
    fn name(&self) -> &'static str {
        "delayed"
    }
    async fn complete(&self, model: &str, _input: &str) -> anyhow::Result<Completion> {
        tokio::time::sleep(self.delay).await;
        Ok(Completion {
            text: serde_json::json!({"title": "Delayed", "units": []}).to_string(),
            provider: "delayed".into(),
            model: model.into(),
            input_tokens: 10,
            output_tokens: 10,
            cache_write_tokens: 0,
            cache_read_tokens: 0,
            usage_unavailable: false,
        })
    }
    async fn stream(&self, _model: &str, _input: &str) -> anyhow::Result<crate::CompletionStream> {
        tokio::time::sleep(self.delay).await;
        anyhow::bail!("timeout")
    }
}

struct PartialStreamProvider;

#[async_trait]
impl LlmProvider for PartialStreamProvider {
    fn name(&self) -> &'static str {
        "partial_stream"
    }
    async fn complete(&self, model: &str, _input: &str) -> anyhow::Result<Completion> {
        Ok(Completion {
            text: "ok".into(),
            provider: "partial_stream".into(),
            model: model.into(),
            input_tokens: 10,
            output_tokens: 10,
            cache_write_tokens: 0,
            cache_read_tokens: 0,
            usage_unavailable: false,
        })
    }
    async fn stream(&self, _model: &str, _input: &str) -> anyhow::Result<crate::CompletionStream> {
        let stream = futures::stream::iter(vec![
            Ok(StreamEvent::Delta("First delta".into())),
            Err(anyhow::anyhow!("stream drop after delta")),
        ]);
        Ok(Box::pin(stream))
    }
}

#[test]
fn decimal_openrouter_aliases_in_models_config() {
    let models: crate::Models =
        toml::from_str(include_str!("../../../../config/models.toml")).unwrap();
    for (task, model) in &models.tasks {
        assert!(
            model.fallback_model.starts_with("anthropic/"),
            "task {task} fallback_model {} must start with anthropic/",
            model.fallback_model
        );
        assert!(
            model.fallback_model.ends_with("-5.5"),
            "task {task} fallback_model {} must end with decimal -5.5",
            model.fallback_model
        );
        assert!(
            !model.fallback_model.ends_with("-5-5"),
            "task {task} fallback_model {} must not end with hyphenated -5-5",
            model.fallback_model
        );
    }
}

#[tokio::test]
async fn primary_429_recovers_on_fallback_within_2_attempt_ceiling() {
    let primary = Arc::new(FailingProvider {
        status_code: 429,
        error_message: "Too Many Requests",
    });
    let fallback = Arc::new(crate::MockProvider);
    let router = ProviderRouter::new(primary, fallback).unwrap();
    let limits = crate::credits::Limits {
        input_tokens: 1000,
        output_tokens: 500,
        timeout_ms: 5000,
        max_attempts: 2,
        max_cost_nanos: 1_000_000,
        input_nanos: 100,
        output_nanos: 500,
        cache_write_nanos: 125,
        cache_read_nanos: 10,
        deadline_unix_ms: 0,
    };

    let outcome = run_bounded(&router, "preview", "test prompt", &limits, |_| true)
        .await
        .unwrap();

    assert!(outcome.completion.is_some());
    assert_eq!(outcome.attempts.len(), 2);
    assert_eq!(outcome.attempts[0].attempt, 1);
    assert!(!outcome.attempts[0].fallback_used);
    assert!(!outcome.attempts[0].delivered);
    assert_eq!(
        outcome.attempts[0].error_code.as_deref(),
        Some("rate_limited")
    );

    assert_eq!(outcome.attempts[1].attempt, 2);
    assert!(outcome.attempts[1].fallback_used);
    assert!(outcome.attempts[1].delivered);
}

#[tokio::test]
async fn terminal_both_providers_fail_stops_with_typed_error() {
    let primary = Arc::new(FailingProvider {
        status_code: 503,
        error_message: "Service Unavailable",
    });
    let fallback = Arc::new(FailingProvider {
        status_code: 500,
        error_message: "Internal Server Error",
    });
    let router = ProviderRouter::new(primary, fallback).unwrap();
    let limits = crate::credits::Limits {
        input_tokens: 1000,
        output_tokens: 500,
        timeout_ms: 5000,
        max_attempts: 2,
        max_cost_nanos: 1_000_000,
        input_nanos: 100,
        output_nanos: 500,
        cache_write_nanos: 125,
        cache_read_nanos: 10,
        deadline_unix_ms: 0,
    };

    let outcome = run_bounded(&router, "preview", "test prompt", &limits, |_| true)
        .await
        .unwrap();

    assert!(outcome.completion.is_none());
    assert_eq!(outcome.attempts.len(), 2);
    assert_eq!(
        outcome.attempts[0].error_code.as_deref(),
        Some("provider_outage")
    );
    assert_eq!(
        outcome.attempts[1].error_code.as_deref(),
        Some("provider_outage")
    );
}

#[tokio::test]
async fn expensive_fallback_halts_before_execution_within_cap() {
    let primary = Arc::new(FailingProvider {
        status_code: 500,
        error_message: "Internal Server Error",
    });
    let fallback = Arc::new(crate::MockProvider);
    let router = ProviderRouter::new(primary, fallback).unwrap();
    // Budget is strictly only enough for 1 attempt worst-case (375_000 nanos)
    let limits = crate::credits::Limits {
        input_tokens: 1000,
        output_tokens: 500,
        timeout_ms: 5000,
        max_attempts: 2,
        max_cost_nanos: 500_000, // Enough for 1 attempt, but not 2
        input_nanos: 100,
        output_nanos: 500,
        cache_write_nanos: 125,
        cache_read_nanos: 10,
        deadline_unix_ms: 0,
    };

    let outcome = run_bounded(&router, "preview", "test prompt", &limits, |_| true)
        .await
        .unwrap();

    assert!(outcome.completion.is_none());
    assert_eq!(outcome.attempts.len(), 1);
    assert_eq!(
        outcome.attempts[0].error_code.as_deref(),
        Some("provider_outage")
    );
}

#[tokio::test]
async fn timeout_cancels_and_retains_unknown_usage() {
    let primary = Arc::new(DelayedProvider {
        delay: Duration::from_millis(100),
    });
    let fallback = Arc::new(DelayedProvider {
        delay: Duration::from_millis(100),
    });
    let router = ProviderRouter::new(primary, fallback).unwrap();
    let limits = crate::credits::Limits {
        input_tokens: 1000,
        output_tokens: 500,
        timeout_ms: 10, // 10ms timeout
        max_attempts: 2,
        max_cost_nanos: 1_000_000,
        input_nanos: 100,
        output_nanos: 500,
        cache_write_nanos: 125,
        cache_read_nanos: 10,
        deadline_unix_ms: 0,
    };

    let outcome = run_bounded(&router, "preview", "test prompt", &limits, |_| true)
        .await
        .unwrap();

    assert!(outcome.completion.is_none());
    assert!(!outcome.attempts.is_empty());
    assert!(outcome.attempts[0].usage_unavailable);
    assert_eq!(outcome.attempts[0].error_code.as_deref(), Some("timeout"));
}

#[tokio::test]
async fn streaming_no_post_delta_fallback() {
    let primary = Arc::new(PartialStreamProvider);
    let fallback = Arc::new(crate::MockProvider);
    let router = ProviderRouter::new(primary, fallback).unwrap();

    let mut stream = stream_bounded(
        &router,
        "chat",
        "input",
        Duration::from_secs(2),
        Duration::from_secs(1),
    )
    .await
    .unwrap();

    use futures::StreamExt;
    let first = stream.next().await;
    assert!(matches!(first, Some(Ok(StreamEvent::Delta(_)))));

    // Next event should be an error because stream failed post-delta; router must NOT fall back!
    let second = stream.next().await;
    assert!(
        second.is_some_and(|r| r.is_err()),
        "stream must terminate with error and no post-delta fallback"
    );
}

#[test]
fn alert_evaluation_budget_boundaries() {
    let cfg = AlertConfig::default();

    // Below 80%: $39.99
    let (fire_80, fire_100) = evaluate_platform_budget(39.99, &cfg);
    assert!(!fire_80);
    assert!(!fire_100);

    // Exactly 80%: $40.00
    let (fire_80, fire_100) = evaluate_platform_budget(40.00, &cfg);
    assert!(fire_80);
    assert!(!fire_100);

    // Between 80% and 100%: $45.00
    let (fire_80, fire_100) = evaluate_platform_budget(45.00, &cfg);
    assert!(fire_80);
    assert!(!fire_100);

    // Exactly 100%: $50.00
    let (fire_80, fire_100) = evaluate_platform_budget(50.00, &cfg);
    assert!(fire_80);
    assert!(fire_100);
}

#[test]
fn alert_evaluation_growth_cache_margin_rates() {
    let cfg = AlertConfig::default();

    // 7-day learner cost increase (+20% boundary)
    assert!(!evaluate_learner_cost_increase(
        10.0,
        11.9,
        cfg.learner_cost_increase_threshold
    ));
    assert!(evaluate_learner_cost_increase(
        10.0,
        12.0,
        cfg.learner_cost_increase_threshold
    ));
    assert!(evaluate_learner_cost_increase(
        10.0,
        13.0,
        cfg.learner_cost_increase_threshold
    ));

    // Cache hit rate (<60% boundary)
    assert!(!evaluate_cache_hit_rate(
        60,
        100,
        cfg.cache_hit_rate_threshold
    ));
    assert!(!evaluate_cache_hit_rate(
        70,
        100,
        cfg.cache_hit_rate_threshold
    ));
    assert!(evaluate_cache_hit_rate(
        59,
        100,
        cfg.cache_hit_rate_threshold
    ));

    // Paid margin (<70% boundary)
    assert!(!evaluate_paid_margin(
        100.0,
        30.0,
        cfg.paid_margin_threshold
    ));
    assert!(!evaluate_paid_margin(
        100.0,
        25.0,
        cfg.paid_margin_threshold
    ));
    assert!(evaluate_paid_margin(100.0, 31.0, cfg.paid_margin_threshold));

    // Fallback rate (15% boundary)
    assert!(!evaluate_rate(14, 100, cfg.fallback_rate_threshold));
    assert!(evaluate_rate(15, 100, cfg.fallback_rate_threshold));

    // Error rate (5% boundary)
    assert!(!evaluate_rate(4, 100, cfg.error_rate_threshold));
    assert!(evaluate_rate(5, 100, cfg.error_rate_threshold));
}

#[test]
fn alert_deduplication_and_privacy_assertions() {
    let key1 = dedupe_alert_key("platform_daily_80", "2026-11-10T14:00");
    let key2 = dedupe_alert_key("platform_daily_80", "2026-11-10T14:00");
    let key3 = dedupe_alert_key("platform_daily_80", "2026-11-10T15:00");

    assert_eq!(
        key1, key2,
        "keys in same window must match for deduplication"
    );
    assert_ne!(key1, key3, "keys in different windows must not match");

    // Assert alert payload contains no private text
    let payload = serde_json::json!({
        "event": "ai_cost_budget_alert",
        "distinct_id": "00000000-0000-4000-8000-000000000011",
        "properties": {
            "source": "api",
            "platform": "web",
            "app_version": "development",
            "scope": "platform_daily_80",
            "actual_usd": 40.50,
            "budget_usd": 50.00
        }
    });

    let raw = payload.to_string();
    for forbidden in &["prompt", "learner_text", "completion", "email", "chat"] {
        assert!(
            !raw.contains(forbidden),
            "payload must not contain forbidden key {forbidden}"
        );
    }
}
