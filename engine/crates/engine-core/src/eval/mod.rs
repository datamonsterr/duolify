//! Model routing and evaluation bounds.
use crate::{
    CompletionStream, ProviderRouter, StreamEvent,
    credits::{Attempt, Limits, Outcome, check_usage, validate_limits},
};
use anyhow::{Context, Result, bail};
use futures::StreamExt;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

#[cfg(test)]
mod tests;

#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub struct AlertConfig {
    pub platform_daily_budget_usd: f64,
    pub daily_budget_alert_80_percent: bool,
    pub daily_budget_alert_100_percent: bool,
    pub learner_cost_increase_threshold: f64,
    pub cache_hit_rate_threshold: f64,
    pub paid_margin_threshold: f64,
    pub fallback_rate_threshold: f64,
    pub error_rate_threshold: f64,
}

impl Default for AlertConfig {
    fn default() -> Self {
        Self {
            platform_daily_budget_usd: 50.0,
            daily_budget_alert_80_percent: true,
            daily_budget_alert_100_percent: true,
            learner_cost_increase_threshold: 0.20,
            cache_hit_rate_threshold: 0.60,
            paid_margin_threshold: 0.70,
            fallback_rate_threshold: 0.15,
            error_rate_threshold: 0.05,
        }
    }
}

pub fn evaluate_platform_budget(actual_spent_usd: f64, cfg: &AlertConfig) -> (bool, bool) {
    let fire_80 = cfg.daily_budget_alert_80_percent
        && actual_spent_usd >= 0.80 * cfg.platform_daily_budget_usd;
    let fire_100 = cfg.daily_budget_alert_100_percent
        && actual_spent_usd >= 1.00 * cfg.platform_daily_budget_usd;
    (fire_80, fire_100)
}

pub fn evaluate_learner_cost_increase(prior_avg: f64, current_avg: f64, threshold: f64) -> bool {
    if prior_avg <= 0.0 {
        return false;
    }
    let growth = (current_avg - prior_avg) / prior_avg;
    growth >= threshold
}

pub fn evaluate_cache_hit_rate(cache_read_tokens: u64, total_tokens: u64, threshold: f64) -> bool {
    if total_tokens == 0 {
        return false;
    }
    let rate = (cache_read_tokens as f64) / (total_tokens as f64);
    rate < threshold
}

pub fn evaluate_paid_margin(revenue: f64, cost: f64, threshold: f64) -> bool {
    if revenue <= 0.0 {
        return false;
    }
    let margin = (revenue - cost) / revenue;
    margin < threshold
}

pub fn evaluate_rate(count: u64, total: u64, threshold: f64) -> bool {
    if total == 0 {
        return false;
    }
    let rate = (count as f64) / (total as f64);
    rate >= threshold
}

pub fn dedupe_alert_key(scope: &str, window_id: &str) -> String {
    format!("{}:{}", scope, window_id)
}

pub fn sanitize_error_code(err_str: &str) -> &'static str {
    let lower = err_str.to_lowercase();
    if lower.contains("429")
        || lower.contains("rate_limited")
        || lower.contains("too many requests")
    {
        "rate_limited"
    } else if lower.contains("500")
        || lower.contains("502")
        || lower.contains("503")
        || lower.contains("504")
        || lower.contains("provider_outage")
        || lower.contains("internal server error")
        || lower.contains("service unavailable")
    {
        "provider_outage"
    } else if lower.contains("timeout") || lower.contains("deadline") || lower.contains("timed out")
    {
        "timeout"
    } else if lower.contains("invalid_schema")
        || lower.contains("invalid json")
        || lower.contains("missing title")
        || lower.contains("missing slides")
        || lower.contains("invalid slide")
    {
        "invalid_schema"
    } else if lower.contains("expensive_fallback") {
        "expensive_fallback"
    } else if lower.contains("cancel") {
        "cancelled"
    } else {
        "provider_failure"
    }
}

/// Execute a task with strict bounds:
/// - Maximum 1 repair/fallback after primary (at most 2 attempts total)
/// - Hard USD ceiling check preventing expensive fallback
/// - Deadline enforcement per attempt and overall task
/// - Schema validation preventing unsafe or malformed delivery
pub async fn run_bounded<F: Fn(&str) -> bool>(
    router: &ProviderRouter,
    task: &str,
    input: &str,
    l: &Limits,
    validate: F,
) -> Result<Outcome> {
    run_bounded_with_policy(router, task, input, l, validate, true).await
}

pub async fn run_bounded_with_policy<F: Fn(&str) -> bool>(
    router: &ProviderRouter,
    task: &str,
    input: &str,
    l: &Limits,
    validate: F,
    reserve_fallback_time: bool,
) -> Result<Outcome> {
    validate_limits(l)?;
    if input.len() > l.input_tokens as usize {
        bail!("input exceeds API envelope")
    }
    let model = router
        .models
        .tasks
        .get(task)
        .ok_or_else(|| anyhow::anyhow!("unknown task"))?;

    let worst = u64::from(l.input_tokens)
        .checked_mul(
            l.input_nanos
                .max(l.cache_write_nanos)
                .max(l.cache_read_nanos),
        )
        .and_then(|n| n.checked_add(u64::from(l.output_tokens).checked_mul(l.output_nanos)?))
        .ok_or_else(|| anyhow::anyhow!("cost overflow"))?;

    let mut outcome = Outcome {
        completion: None,
        attempts: vec![],
    };
    let mut spent = 0u64;
    let started = Instant::now();
    let mut budget = Duration::from_millis(u64::from(l.timeout_ms));
    if l.deadline_unix_ms != 0 {
        let now = SystemTime::now().duration_since(UNIX_EPOCH)?.as_millis();
        let remaining = u128::from(l.deadline_unix_ms).saturating_sub(now);
        // This conversion is bounded by the relative API timeout (u32).
        budget = budget.min(Duration::from_millis(
            remaining.min(u128::from(l.timeout_ms)) as u64,
        ));
    }

    // Launch policy: at most 2 attempts total (primary + at most 1 repair/fallback)
    let max_attempts = l.max_attempts.min(2);

    for index in 0..max_attempts {
        let fallback = index > 0;
        let route = if fallback {
            &model.fallback_model
        } else {
            &model.model
        };
        let provider = if fallback {
            &router.fallback
        } else {
            &router.primary
        };

        if spent
            .checked_add(worst)
            .is_none_or(|n| n > l.max_cost_nanos)
        {
            // Fallback would exceed USD cap; halt without calling fallback
            break;
        }

        let remaining = budget.saturating_sub(started.elapsed());
        if remaining.is_zero() {
            break;
        }

        let attempt_started = Instant::now();
        let timeout_duration = if reserve_fallback_time {
            remaining / (max_attempts - index)
        } else {
            remaining
        };

        let response = tokio::time::timeout(
            timeout_duration,
            provider.complete_bounded(route, input, l.output_tokens),
        )
        .await;

        let latency_ms = attempt_started
            .elapsed()
            .as_millis()
            .min(u128::from(l.timeout_ms)) as u32;

        match response {
            Ok(Ok(c)) => {
                let mut attempt = Attempt {
                    provider: provider.name().into(),
                    model: route.clone(),
                    input_tokens: c.input_tokens,
                    output_tokens: c.output_tokens,
                    cache_write_tokens: c.cache_write_tokens,
                    cache_read_tokens: c.cache_read_tokens,
                    latency_ms,
                    attempt: index + 1,
                    fallback_used: fallback,
                    delivered: false,
                    usage_unavailable: c.usage_unavailable,
                    error_code: None,
                };

                let cost_res = check_usage(
                    l,
                    c.input_tokens,
                    c.output_tokens,
                    c.cache_write_tokens,
                    c.cache_read_tokens,
                );

                match cost_res {
                    Ok(actual_cost) => {
                        let cost = if c.usage_unavailable {
                            worst
                        } else {
                            actual_cost
                        };
                        spent += cost;
                        if validate(&c.text) {
                            attempt.delivered = true;
                            outcome.completion = Some(c);
                            outcome.attempts.push(attempt);
                            break;
                        } else {
                            attempt.error_code = Some("invalid_schema".into());
                        }
                    }
                    Err(_) => {
                        spent += worst;
                        attempt.error_code = Some("usage_outside_envelope".into());
                    }
                }
                outcome.attempts.push(attempt);
            }
            Ok(Err(e)) => {
                spent += worst;
                let code = sanitize_error_code(&e.to_string());
                outcome.attempts.push(Attempt {
                    provider: provider.name().into(),
                    model: route.clone(),
                    input_tokens: 0,
                    output_tokens: 0,
                    cache_write_tokens: 0,
                    cache_read_tokens: 0,
                    latency_ms,
                    attempt: index + 1,
                    fallback_used: fallback,
                    delivered: false,
                    usage_unavailable: true,
                    error_code: Some(code.into()),
                });
            }
            Err(_) => {
                // Timeout
                spent += worst;
                outcome.attempts.push(Attempt {
                    provider: provider.name().into(),
                    model: route.clone(),
                    input_tokens: 0,
                    output_tokens: 0,
                    cache_write_tokens: 0,
                    cache_read_tokens: 0,
                    latency_ms,
                    attempt: index + 1,
                    fallback_used: fallback,
                    delivered: false,
                    usage_unavailable: true,
                    error_code: Some("timeout".into()),
                });
            }
        }
    }

    Ok(outcome)
}

/// Stream with first-token timeout, idle timeout, and no post-delta fallback.
pub async fn stream_bounded(
    router: &ProviderRouter,
    task: &str,
    input: &str,
    first_token_timeout: Duration,
    idle_timeout: Duration,
) -> Result<CompletionStream> {
    let model = router.models.tasks.get(task).context("unknown task")?;

    let primary_res = router.primary.stream(&model.model, input).await;

    let underlying = match primary_res {
        Ok(s) => s,
        Err(_) => {
            // Fall back before any delta has been emitted
            router.fallback.stream(&model.fallback_model, input).await?
        }
    };

    let stream = futures::stream::unfold(
        (underlying, false, true, first_token_timeout, idle_timeout),
        move |(mut underlying, mut emitted_delta, first_check, first_to, idle_to)| async move {
            let timeout = if first_check { first_to } else { idle_to };
            match tokio::time::timeout(timeout, underlying.next()).await {
                Ok(Some(Ok(event))) => {
                    if let StreamEvent::Delta(_) = &event {
                        emitted_delta = true;
                    }
                    Some((
                        Ok(event),
                        (underlying, emitted_delta, false, first_to, idle_to),
                    ))
                }
                Ok(Some(Err(e))) => {
                    // Post-delta error terminates stream without fallback
                    Some((
                        Err(e),
                        (underlying, emitted_delta, false, first_to, idle_to),
                    ))
                }
                Ok(None) => None,
                Err(_) => {
                    // Timeout
                    Some((
                        Err(anyhow::anyhow!("stream timeout")),
                        (underlying, emitted_delta, false, first_to, idle_to),
                    ))
                }
            }
        },
    );

    Ok(Box::pin(stream))
}
