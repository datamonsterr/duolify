//! API-priced envelopes. The engine contains no commercial amounts.
use crate::{Completion, ProviderRouter};
use anyhow::{Result, bail};
use serde::{Deserialize, Serialize};

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
#[derive(Debug, Clone, Serialize)]
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
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error_code: Option<String>,
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
    crate::eval::run_bounded_with_policy(router, task, input, l, validate, false).await
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
    crate::eval::run_bounded_with_policy(router, task, input, l, validate, true).await
}

/// Attempt 1 uses `inputs[0]`; the evaluated fallback attempt uses `inputs[1]`.
pub async fn run_validated_inputs<F: Fn(&str) -> bool>(
    router: &ProviderRouter,
    task: &str,
    inputs: &[&str],
    l: &Limits,
    validate: F,
) -> Result<Outcome> {
    crate::eval::run_bounded_inputs(router, task, inputs, l, validate, true).await
}

#[cfg(test)]
mod tests;
