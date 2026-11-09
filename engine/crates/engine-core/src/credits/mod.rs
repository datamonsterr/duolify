//! API-priced envelopes. The engine contains no commercial amounts.
use crate::{Completion, ProviderRouter};
use anyhow::{Result, bail};
use serde::{Deserialize, Serialize};
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

#[derive(Debug, Default, Clone, Deserialize, Serialize)]
pub struct Limits {
    pub input_tokens: u32,
    pub output_tokens: u32,
    pub timeout_ms: u32,
    pub max_attempts: u32,
    pub max_cost_nanos: u64,
    pub input_nanos: u64,
    pub output_nanos: u64,
    pub cache_write_nanos: u64,
    pub cache_read_nanos: u64,
    #[serde(default)]
    pub deadline_unix_ms: u64,
}
pub fn validate_limits(l: &Limits) -> Result<()> {
    if l.input_tokens == 0
        || l.output_tokens == 0
        || l.timeout_ms == 0
        || l.max_attempts == 0
        || l.max_cost_nanos == 0
        || l.input_nanos == 0
        || l.output_nanos == 0
        || l.cache_write_nanos == 0
        || l.cache_read_nanos == 0
    {
        bail!("missing API usage bounds");
    }
    Ok(())
}
pub fn check_usage(l: &Limits, input: u32, output: u32, write: u32, read: u32) -> Result<u64> {
    if u64::from(input) + u64::from(write) + u64::from(read) > u64::from(l.input_tokens)
        || output > l.output_tokens
    {
        bail!("usage envelope exceeded")
    }
    let cost = [
        (input, l.input_nanos),
        (output, l.output_nanos),
        (write, l.cache_write_nanos),
        (read, l.cache_read_nanos),
    ]
    .into_iter()
    .try_fold(0u64, |sum, (n, rate)| {
        sum.checked_add(u64::from(n).checked_mul(rate)?)
    })
    .ok_or_else(|| anyhow::anyhow!("cost overflow"))?;
    if cost > l.max_cost_nanos {
        bail!("USD envelope exceeded")
    }
    Ok(cost)
}
#[derive(Debug, Serialize)]
pub struct Attempt {
    pub provider: String,
    pub model: String,
    pub input_tokens: u32,
    pub output_tokens: u32,
    pub cache_write_tokens: u32,
    pub cache_read_tokens: u32,
    pub latency_ms: u32,
    pub attempt: u32,
    pub fallback_used: bool,
    pub delivered: bool,
    pub usage_unavailable: bool,
}
pub struct Outcome {
    pub completion: Option<Completion>,
    pub attempts: Vec<Attempt>,
}

/// A caller must supply its required schema; JSON syntax alone is insufficient.
pub async fn run<F: Fn(&str) -> bool>(
    router: &ProviderRouter,
    task: &str,
    input: &str,
    l: &Limits,
    validate: F,
) -> Result<Outcome> {
    execute(router, task, input, l, validate, false).await
}

/// Feature validation participates in the same attempt, USD and deadline budget.
/// Preview leaves time for its evaluated fallback within the total deadline.
/// Errors, timeouts and missing usage retain a conservative cost hold.
pub async fn run_validated<F: Fn(&str) -> bool>(
    router: &ProviderRouter,
    task: &str,
    input: &str,
    l: &Limits,
    validate: F,
) -> Result<Outcome> {
    execute(router, task, input, l, validate, true).await
}
async fn execute<F: Fn(&str) -> bool>(
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
    for index in 0..l.max_attempts {
        if spent
            .checked_add(worst)
            .is_none_or(|n| n > l.max_cost_nanos)
        {
            break;
        }
        let remaining = budget.saturating_sub(started.elapsed());
        if remaining.is_zero() {
            break;
        }
        let fallback = index > 0;
        let provider = if fallback {
            &router.fallback
        } else {
            &router.primary
        };
        let route = if fallback {
            &model.fallback_model
        } else {
            &model.model
        };
        let attempt_started = Instant::now();
        let response = tokio::time::timeout(
            if reserve_fallback_time {
                remaining / (l.max_attempts - index)
            } else {
                remaining
            },
            provider.complete_bounded(route, input, l.output_tokens),
        )
        .await;
        let completion = match response {
            Ok(Ok(c)) => Some(c),
            _ => None,
        };
        let mut attempt = Attempt {
            provider: provider.name().into(),
            model: route.clone(),
            input_tokens: 0,
            output_tokens: 0,
            cache_write_tokens: 0,
            cache_read_tokens: 0,
            latency_ms: attempt_started
                .elapsed()
                .as_millis()
                .min(u128::from(l.timeout_ms)) as u32,
            attempt: index + 1,
            fallback_used: fallback,
            delivered: false,
            usage_unavailable: completion.as_ref().is_none_or(|c| c.usage_unavailable),
        };
        let mut cost = worst;
        if let Some(c) = completion {
            attempt.input_tokens = c.input_tokens;
            attempt.output_tokens = c.output_tokens;
            attempt.cache_write_tokens = c.cache_write_tokens;
            attempt.cache_read_tokens = c.cache_read_tokens;
            if let Ok(actual) = check_usage(
                l,
                c.input_tokens,
                c.output_tokens,
                c.cache_write_tokens,
                c.cache_read_tokens,
            ) {
                if !c.usage_unavailable {
                    cost = actual;
                }
                // Invalid structured output is undelivered and can use one bounded fallback.
                if validate(&c.text) {
                    attempt.delivered = true;
                    outcome.completion = Some(c)
                }
            }
        }
        spent += cost;
        outcome.attempts.push(attempt);
        if outcome.completion.is_some() {
            break;
        }
    }
    Ok(outcome)
}
#[cfg(test)]
mod tests;
