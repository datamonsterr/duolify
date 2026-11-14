//! A Lesson beyond the published snapshot of a Course copy (E15-04). The API owns the gates
//! (copy ownership, previous pass, consent, balance); this module re-checks them
//! deterministically before any provider call and runs the same bounded, schema-checked,
//! one-fallback generation as every other Lesson task.
use crate::{
    ProviderRouter,
    credits::{Limits, Outcome},
};

/// Facts the API asserts about the request. Credits are the quote's ceilings, not prices.
#[derive(Debug, Clone, serde::Deserialize)]
pub struct MissingGate {
    pub snapshot_lessons: u32,
    pub lesson_index: u32,
    pub previous_passed: bool,
    pub consent: bool,
    pub quote_credits: u64,
    pub balance_credits: u64,
}

const MAX_TIMEOUT_MS: u32 = 300_000;

/// Deterministic envelope check; runs before any provider call. Error text is a closed code.
pub fn check_missing(g: &MissingGate, l: &Limits) -> anyhow::Result<()> {
    anyhow::ensure!(g.lesson_index >= g.snapshot_lessons, "not_beyond_snapshot");
    anyhow::ensure!(g.previous_passed, "previous_not_passed");
    anyhow::ensure!(g.consent, "consent_required");
    anyhow::ensure!(g.balance_credits >= g.quote_credits, "insufficient_credits");
    crate::credits::validate_limits(l)?;
    anyhow::ensure!(
        l.max_attempts <= 2 && l.timeout_ms <= MAX_TIMEOUT_MS,
        "invalid_envelope"
    );
    Ok(())
}

/// The full Lesson schema (slides, test, variant pool), not just JSON syntax.
pub fn lesson_ok(raw: &str) -> bool {
    serde_json::from_str::<serde_json::Value>(raw)
        .is_ok_and(|v| crate::lessontest::validate_lesson(&v).is_ok())
}

pub async fn generate_missing(
    router: &ProviderRouter,
    g: &MissingGate,
    input_json: &str,
    l: &Limits,
) -> anyhow::Result<Outcome> {
    check_missing(g, l)?;
    crate::credits::run_validated(router, "published_copy_lesson", input_json, l, lesson_ok).await
}
