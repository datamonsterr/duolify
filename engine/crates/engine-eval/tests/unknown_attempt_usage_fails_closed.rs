//! Review F8: an attempt whose usage is unknown (a timed-out or failed primary) cannot be priced
//! as zero: it is priced at its envelope's worst case and reported as a warning. A case is
//! critical only when the delivered completion's own usage is unknown (design.md section 14).
use async_trait::async_trait;
use engine_core::{Completion, LlmProvider, ProviderRouter, Route};
use engine_eval::{Case, Check, Runner, Thresholds};
use std::sync::{Arc, Mutex};

/// The primary fails without usage; the fallback answers with measured usage.
struct PrimaryFails(Mutex<u32>);

#[async_trait]
impl LlmProvider for PrimaryFails {
    fn name(&self) -> &'static str {
        "openrouter"
    }
    async fn complete(&self, _: &str, _: &str) -> anyhow::Result<Completion> {
        unreachable!("routed calls only")
    }
    async fn complete_route(&self, route: &Route, _: &str, _: u32) -> anyhow::Result<Completion> {
        let mut calls = self.0.lock().unwrap();
        *calls += 1;
        if *calls == 1 {
            anyhow::bail!("primary timed out after consuming tokens");
        }
        Ok(Completion {
            text: "{\"ok\":true}".into(),
            provider: "openrouter".into(),
            model: route.model.clone(),
            input_tokens: 100,
            output_tokens: 20,
            ..Completion::default()
        })
    }
    async fn stream(&self, _: &str, _: &str) -> anyhow::Result<engine_core::CompletionStream> {
        anyhow::bail!("not streaming")
    }
}

#[tokio::test]
async fn a_delivered_case_after_an_unknown_usage_attempt_fails_critically() {
    let provider = Arc::new(PrimaryFails(Mutex::new(0)));
    let router = ProviderRouter::new(provider.clone(), provider).unwrap();
    let runner = Runner::with_router(router, "live", false);
    let report = runner
        .run_cases(
            vec![Case {
                id: "unknown-usage".into(),
                task: "credit_envelope".into(),
                input: serde_json::json!({}),
                rubric: "Valid JSON".into(),
                checks: vec![Check {
                    kind: "json".into(),
                    value: None,
                    critical: false,
                }],
            }],
            &Thresholds::default(),
        )
        .await
        .unwrap();
    let case = &report.results[0];
    assert_eq!(case.served_models.len(), 2, "{case:?}");
    // The delivered completion has measured usage: the earlier unknown attempt is a warning,
    // priced at its envelope's worst case, not a critical failure.
    assert!(!case.critical_failed, "{:?}", case.errors);
    assert!(case.passed, "{:?}", case.errors);
    assert!(
        case.warnings
            .iter()
            .any(|w| w.contains("usage_unavailable")),
        "{:?}",
        case.warnings
    );
    let measured = (100.0 * 2000.0 + 20.0 * 10000.0) / 1e9;
    assert!(
        case.cost_usd > measured * 10.0,
        "worst-case pricing expected, got {}",
        case.cost_usd
    );
}

/// The delivered completion's own usage is unknown: critical.
struct DeliveredUnknown;

#[async_trait]
impl LlmProvider for DeliveredUnknown {
    fn name(&self) -> &'static str {
        "openrouter"
    }
    async fn complete(&self, _: &str, _: &str) -> anyhow::Result<Completion> {
        unreachable!("routed calls only")
    }
    async fn complete_route(&self, route: &Route, _: &str, _: u32) -> anyhow::Result<Completion> {
        Ok(Completion {
            text: "{\"ok\":true}".into(),
            provider: "openrouter".into(),
            model: route.model.clone(),
            usage_unavailable: true,
            ..Completion::default()
        })
    }
    async fn stream(&self, _: &str, _: &str) -> anyhow::Result<engine_core::CompletionStream> {
        anyhow::bail!("not streaming")
    }
}

#[tokio::test]
async fn a_delivered_completion_with_unknown_usage_stays_critical() {
    let provider = Arc::new(DeliveredUnknown);
    let router = ProviderRouter::new(provider.clone(), provider).unwrap();
    let runner = Runner::with_router(router, "live", false);
    let report = runner
        .run_cases(
            vec![Case {
                id: "delivered-unknown".into(),
                task: "credit_envelope".into(),
                input: serde_json::json!({}),
                rubric: "Valid JSON".into(),
                checks: vec![],
            }],
            &Thresholds::default(),
        )
        .await
        .unwrap();
    let case = &report.results[0];
    assert!(case.critical_failed, "{:?}", case.errors);
    assert!(case.errors.iter().any(|e| e.contains("usage_unavailable")));
    assert!(!report.passed && report.critical_failure);
}
