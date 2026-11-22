//! Review F8: an attempt whose usage is unknown (a timed-out or failed primary) cannot be priced
//! as zero. A case whose delivered answer came after such an attempt fails critically, so the
//! report cannot claim to be inside budget and cannot be promoted.
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
    assert!(case.critical_failed, "{:?}", case.errors);
    assert!(
        case.errors.iter().any(|e| e.contains("usage_unavailable")),
        "{:?}",
        case.errors
    );
    assert!(!report.passed && report.critical_failure);
}
