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
fn models_config_uses_only_the_allowlisted_openrouter_ids() {
    const ALLOWED: [&str; 2] = ["tencent/hy4-flash", "z-ai/glm-5.3-flash"];
    let models: crate::Models =
        toml::from_str(include_str!("../../../../config/models.toml")).unwrap();
    for (task, model) in &models.tasks {
        assert!(
            !model.fallback_models.is_empty(),
            "task {task} needs a fallback"
        );
        assert_eq!(model.fallback_model, model.fallback_models[0]);
        if task == "transcribe" {
            assert_eq!(model.model, "qwen/qwen3-asr-0.6b");
            assert_eq!(model.fallback_model, "openai/whisper-large-v3-turbo");
            continue;
        }
        let mut chain = vec![&model.model];
        chain.extend(&model.fallback_models);
        for id in &chain {
            assert!(
                ALLOWED.contains(&id.as_str()),
                "task {task}: {id} not allowlisted"
            );
        }
        let unique: std::collections::BTreeSet<_> = chain.iter().collect();
        assert_eq!(unique.len(), chain.len(), "task {task} repeats a model");
        for id in &chain {
            assert!(
                model.reasoning.contains_key(id.as_str()),
                "{task}: no effort for {id}"
            );
        }
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
        None,
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

struct SlowFirstTokenProvider {
    delay: Duration,
}

#[async_trait]
impl LlmProvider for SlowFirstTokenProvider {
    fn name(&self) -> &'static str {
        "slow_first_token"
    }
    async fn complete(&self, model: &str, _input: &str) -> anyhow::Result<Completion> {
        tokio::time::sleep(self.delay).await;
        Ok(Completion {
            text: "slow".into(),
            provider: "slow_first_token".into(),
            model: model.into(),
            input_tokens: 10,
            output_tokens: 10,
            cache_write_tokens: 0,
            cache_read_tokens: 0,
            usage_unavailable: false,
        })
    }
    async fn stream(&self, _model: &str, _input: &str) -> anyhow::Result<crate::CompletionStream> {
        let delay = self.delay;
        let s = futures::stream::unfold(false, move |emitted| async move {
            if !emitted {
                tokio::time::sleep(delay).await;
                Some((Ok(StreamEvent::Delta("Primary slow token".into())), true))
            } else {
                None
            }
        });
        Ok(Box::pin(s))
    }
}

struct InstantFallbackProvider;

#[async_trait]
impl LlmProvider for InstantFallbackProvider {
    fn name(&self) -> &'static str {
        "instant_fallback"
    }
    async fn complete(&self, model: &str, _input: &str) -> anyhow::Result<Completion> {
        Ok(Completion {
            text: "Fallback ok".into(),
            provider: "instant_fallback".into(),
            model: model.into(),
            input_tokens: 5,
            output_tokens: 5,
            cache_write_tokens: 0,
            cache_read_tokens: 0,
            usage_unavailable: false,
        })
    }
    async fn stream(&self, _model: &str, _input: &str) -> anyhow::Result<crate::CompletionStream> {
        let s = futures::stream::iter(vec![Ok(StreamEvent::Delta("Fallback token".into()))]);
        Ok(Box::pin(s))
    }
}

#[tokio::test]
async fn stream_bounded_falls_back_on_first_token_timeout() {
    let primary = Arc::new(SlowFirstTokenProvider {
        delay: Duration::from_millis(300),
    });
    let fallback = Arc::new(InstantFallbackProvider);
    let router = ProviderRouter::new(primary, fallback).unwrap();

    let mut stream = stream_bounded(
        &router,
        "chat",
        "input",
        None,
        Duration::from_millis(50),
        Duration::from_millis(500),
    )
    .await
    .unwrap();

    use futures::StreamExt;
    let first = stream.next().await;
    match first {
        Some(Ok(StreamEvent::Delta(d))) => {
            assert_eq!(
                d, "Fallback token",
                "must have fallen back to secondary stream"
            );
        }
        other => panic!("expected delta from fallback, got {:?}", other),
    }
}

#[tokio::test]
async fn stream_bounded_honors_cost_and_deadline_and_envelope() {
    let primary = Arc::new(crate::MockProvider);
    let fallback = Arc::new(crate::MockProvider);
    let router = ProviderRouter::new(primary, fallback).unwrap();

    // 1. Cost limit exceeded
    let limits_low_budget = Limits {
        input_tokens: 1000,
        output_tokens: 500,
        timeout_ms: 5000,
        max_attempts: 2,
        max_cost_nanos: 1, // extremely low max cost
        input_nanos: 1000,
        output_nanos: 5000,
        cache_write_nanos: 1250,
        cache_read_nanos: 100,
        deadline_unix_ms: 0,
    };
    let res_cost = stream_bounded(
        &router,
        "chat",
        "input",
        Some(&limits_low_budget),
        Duration::from_secs(2),
        Duration::from_secs(1),
    )
    .await;
    assert!(
        res_cost.is_err(),
        "must reject when worst-case cost exceeds USD cap"
    );

    // 2. Deadline already expired
    let limits_expired = Limits {
        input_tokens: 1000,
        output_tokens: 500,
        timeout_ms: 5000,
        max_attempts: 2,
        max_cost_nanos: 1_000_000_000,
        input_nanos: 1000,
        output_nanos: 5000,
        cache_write_nanos: 1250,
        cache_read_nanos: 100,
        deadline_unix_ms: 100, // 100ms after unix epoch -> long expired
    };
    let res_deadline = stream_bounded(
        &router,
        "chat",
        "input",
        Some(&limits_expired),
        Duration::from_secs(2),
        Duration::from_secs(1),
    )
    .await;
    assert!(
        res_deadline.is_err(),
        "must reject when deadline has expired"
    );

    // 3. Input exceeds envelope
    let limits_small_envelope = Limits {
        input_tokens: 2,
        output_tokens: 500,
        timeout_ms: 5000,
        max_attempts: 2,
        max_cost_nanos: 1_000_000_000,
        input_nanos: 1000,
        output_nanos: 5000,
        cache_write_nanos: 1250,
        cache_read_nanos: 100,
        deadline_unix_ms: 0,
    };
    let res_envelope = stream_bounded(
        &router,
        "chat",
        "input longer than 2 tokens",
        Some(&limits_small_envelope),
        Duration::from_secs(2),
        Duration::from_secs(1),
    )
    .await;
    assert!(
        res_envelope.is_err(),
        "must reject when input exceeds input_tokens envelope"
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

struct HangAfterDelta;

#[async_trait]
impl LlmProvider for HangAfterDelta {
    fn name(&self) -> &'static str {
        "hang_after_delta"
    }
    async fn complete(&self, _: &str, _: &str) -> anyhow::Result<Completion> {
        anyhow::bail!("not used")
    }
    async fn stream(&self, _: &str, _: &str) -> anyhow::Result<crate::CompletionStream> {
        let s = futures::stream::iter(vec![Ok(StreamEvent::Delta("one".into()))])
            .chain(futures::stream::pending());
        Ok(Box::pin(s))
    }
}

struct SilentProvider;

#[async_trait]
impl LlmProvider for SilentProvider {
    fn name(&self) -> &'static str {
        "silent"
    }
    async fn complete(&self, _: &str, _: &str) -> anyhow::Result<Completion> {
        anyhow::bail!("not used")
    }
    async fn stream(&self, _: &str, _: &str) -> anyhow::Result<crate::CompletionStream> {
        Ok(Box::pin(futures::stream::pending()))
    }
}

#[tokio::test]
async fn idle_timeout_after_a_delta_is_typed_and_never_starts_a_second_provider() {
    use futures::StreamExt;
    let router =
        ProviderRouter::new(Arc::new(HangAfterDelta), Arc::new(InstantFallbackProvider)).unwrap();
    let (mut stream, route) = stream_bounded_routed(
        &router,
        "chat",
        "input",
        None,
        Duration::from_millis(200),
        Duration::from_millis(40),
    )
    .await
    .unwrap();
    assert!(!route.fallback_used);
    assert_eq!(route.provider, "hang_after_delta");
    assert!(matches!(stream.next().await, Some(Ok(StreamEvent::Delta(d))) if d == "one"));
    let err = stream.next().await.unwrap().unwrap_err();
    assert_eq!(
        err.downcast_ref::<StreamFailure>(),
        Some(&StreamFailure::IdleTimeout)
    );
}

#[tokio::test]
async fn first_token_timeout_on_primary_uses_the_one_evaluated_fallback() {
    use futures::StreamExt;
    let router =
        ProviderRouter::new(Arc::new(SilentProvider), Arc::new(InstantFallbackProvider)).unwrap();
    let (mut stream, route) = stream_bounded_routed(
        &router,
        "chat",
        "input",
        None,
        Duration::from_millis(40),
        Duration::from_millis(200),
    )
    .await
    .unwrap();
    assert!(route.fallback_used);
    assert_eq!(route.provider, "instant_fallback");
    assert!(matches!(
        stream.next().await,
        Some(Ok(StreamEvent::Delta(_)))
    ));
}

#[tokio::test]
async fn a_silent_fallback_is_a_typed_first_token_timeout_not_a_third_provider() {
    use futures::StreamExt;
    let router = ProviderRouter::new(Arc::new(SilentProvider), Arc::new(SilentProvider)).unwrap();
    let (mut stream, route) = stream_bounded_routed(
        &router,
        "chat",
        "input",
        None,
        Duration::from_millis(30),
        Duration::from_millis(200),
    )
    .await
    .unwrap();
    assert!(route.fallback_used);
    let err = stream.next().await.unwrap().unwrap_err();
    assert_eq!(
        err.downcast_ref::<StreamFailure>(),
        Some(&StreamFailure::FirstTokenTimeout)
    );
}

struct CaptureProvider(Arc<std::sync::atomic::AtomicU32>);

#[async_trait]
impl LlmProvider for CaptureProvider {
    fn name(&self) -> &'static str {
        "capture"
    }
    async fn complete(&self, model: &str, _input: &str) -> anyhow::Result<Completion> {
        Ok(Completion {
            text: "{}".into(),
            provider: "capture".into(),
            model: model.into(),
            input_tokens: 10,
            output_tokens: 10,
            cache_write_tokens: 0,
            cache_read_tokens: 6,
            usage_unavailable: false,
        })
    }
    async fn complete_bounded(
        &self,
        model: &str,
        input: &str,
        max_output: u32,
    ) -> anyhow::Result<Completion> {
        self.0
            .store(max_output, std::sync::atomic::Ordering::SeqCst);
        self.complete(model, input).await
    }
    async fn stream(&self, _model: &str, _input: &str) -> anyhow::Result<crate::CompletionStream> {
        anyhow::bail!("no stream")
    }
}

fn limits_with_output(output_tokens: u32) -> Limits {
    Limits {
        input_tokens: 20_000,
        output_tokens,
        timeout_ms: 5_000,
        max_attempts: 1,
        max_cost_nanos: 100_000_000_000,
        input_nanos: 35,
        output_nanos: 140,
        cache_write_nanos: 35,
        cache_read_nanos: 2,
        deadline_unix_ms: 0,
    }
}

async fn sent_max_tokens(task: &str, system: Option<String>, out: u32) -> u32 {
    let seen = Arc::new(std::sync::atomic::AtomicU32::new(0));
    let p: Arc<dyn LlmProvider> = Arc::new(CaptureProvider(seen.clone()));
    let router = ProviderRouter::new(p.clone(), p).unwrap();
    let shape = Shape {
        system,
        ..Default::default()
    };
    run_bounded_formatted(
        &router,
        task,
        &["data"],
        &limits_with_output(out),
        shape,
        |_| Ok(()),
        true,
    )
    .await
    .unwrap();
    seen.load(std::sync::atomic::Ordering::SeqCst)
}

#[tokio::test]
async fn routed_calls_cap_max_tokens_and_monolithic_calls_keep_the_quote_ceiling() {
    // Monolithic: the quote's ceiling, unchanged.
    assert_eq!(sent_max_tokens("lesson", None, 9_000).await, 9_000);
    // Routed lesson: the 6,000 cap; a lower quote ceiling still wins; the minimum holds.
    assert_eq!(
        sent_max_tokens("lesson", Some("core".into()), 9_000).await,
        6_000
    );
    assert_eq!(
        sent_max_tokens("lesson", Some("core".into()), 800).await,
        800
    );
    assert_eq!(
        sent_max_tokens("lesson", Some("core".into()), 1_500).await,
        1_500
    );
    // Other routed tasks keep the quote ceiling.
    assert_eq!(
        sent_max_tokens("preview", Some("core".into()), 7_000).await,
        7_000
    );
}

#[test]
fn routed_max_tokens_respects_the_usd_ceiling_and_the_minimum() {
    let mut l = limits_with_output(9_000);
    // Room for 3,000 output tokens only: 3,000 * 140 + 1,000 * 35 = 455,000.
    l.max_cost_nanos = 455_000;
    assert_eq!(routed_max_tokens("lesson", &l, 1_000), 3_000);
    // Almost no room: the task minimum applies, bounded by the quote ceiling.
    l.max_cost_nanos = 1_000;
    assert_eq!(routed_max_tokens("lesson", &l, 1_000), ROUTED_OUTPUT_MIN);
}
