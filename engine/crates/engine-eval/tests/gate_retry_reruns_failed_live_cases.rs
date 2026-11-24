//! Gate retry (owner decision 2026-11-24, design.md section 14): a failed live case is re-run once.
//! Both attempts are recorded, the final outcome decides, a pass on retry is flagged, the budget
//! covers both attempts, and offline runs never retry.
use async_trait::async_trait;
use engine_core::{Completion, LlmProvider, ProviderRouter, Route};
use engine_eval::{Case, Check, Runner, Thresholds};
use std::sync::{Arc, Mutex};

/// Replies with the scripted texts in order, then repeats the last one.
struct Scripted(Mutex<Vec<&'static str>>, Mutex<u32>);

#[async_trait]
impl LlmProvider for Scripted {
    fn name(&self) -> &'static str {
        "openrouter"
    }
    async fn complete(&self, _: &str, _: &str) -> anyhow::Result<Completion> {
        unreachable!("routed calls only")
    }
    async fn complete_route(&self, route: &Route, _: &str, _: u32) -> anyhow::Result<Completion> {
        let mut n = self.1.lock().unwrap();
        let texts = self.0.lock().unwrap();
        let text = texts[(*n as usize).min(texts.len() - 1)];
        *n += 1;
        Ok(Completion {
            text: text.into(),
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

fn runner(texts: Vec<&'static str>, retry: bool) -> (Runner, Arc<Scripted>) {
    let provider = Arc::new(Scripted(Mutex::new(texts), Mutex::new(0)));
    let router = ProviderRouter::new(provider.clone(), provider.clone()).unwrap();
    let mut r = Runner::with_router(router, "live", false);
    r.retry_failed = retry;
    (r, provider)
}

fn case(id: &str) -> Case {
    Case {
        id: id.into(),
        task: "credit_envelope".into(),
        input: serde_json::json!({}),
        rubric: String::new(),
        checks: vec![Check {
            kind: "contains".into(),
            value: Some("GOOD".into()),
            critical: false,
        }],
    }
}

#[tokio::test]
async fn a_case_that_passes_only_on_retry_is_flagged_and_records_both_attempts() {
    let (r, p) = runner(vec!["{\"x\":\"BAD\"}", "{\"x\":\"GOOD\"}"], true);
    let report = r
        .run_cases(vec![case("c1")], &Thresholds::default())
        .await
        .unwrap();
    let c = &report.results[0];
    assert!(c.passed && c.retried, "{c:?}");
    assert_eq!(c.attempts.len(), 2);
    assert_eq!(c.attempts[0].attempt, 1);
    assert_eq!(c.attempts[0].outcome, "failed");
    assert_eq!(c.attempts[1].outcome, "passed");
    assert!(c.attempts[0].cost_usd > 0.0 && c.attempts[1].cost_usd > 0.0);
    assert!((c.cost_usd - c.attempts[0].cost_usd - c.attempts[1].cost_usd).abs() < 1e-12);
    assert_eq!(report.retried_cases, 1);
    assert!(report.retry_summary().unwrap().contains("1 case"));
    assert_eq!(*p.1.lock().unwrap(), 2);
}

#[tokio::test]
async fn a_case_failing_twice_fails_the_report_with_both_attempts_recorded() {
    let (r, p) = runner(vec!["{\"x\":\"BAD\"}"], true);
    let report = r
        .run_cases(vec![case("c1")], &Thresholds::default())
        .await
        .unwrap();
    let c = &report.results[0];
    assert!(!c.passed && !c.retried, "{c:?}");
    assert_eq!(c.attempts.len(), 2);
    assert!(!report.passed);
    assert_eq!(report.retried_cases, 0);
    assert_eq!(*p.1.lock().unwrap(), 2, "exactly one retry");
}

#[tokio::test]
async fn a_passing_case_is_not_rerun() {
    let (r, p) = runner(vec!["{\"x\":\"GOOD\"}"], true);
    let report = r
        .run_cases(vec![case("c1")], &Thresholds::default())
        .await
        .unwrap();
    assert!(report.results[0].attempts.is_empty() && !report.results[0].retried);
    assert_eq!(*p.1.lock().unwrap(), 1);
}

#[tokio::test]
async fn retry_is_off_unless_enabled() {
    let (r, p) = runner(vec!["{\"x\":\"BAD\"}"], false);
    let report = r
        .run_cases(vec![case("c1")], &Thresholds::default())
        .await
        .unwrap();
    assert!(!report.results[0].passed && report.results[0].attempts.is_empty());
    assert_eq!(*p.1.lock().unwrap(), 1);
}

#[tokio::test]
async fn the_budget_cap_covers_both_attempts() {
    let (mut r, _) = runner(vec!["{\"x\":\"BAD\"}", "{\"x\":\"GOOD\"}"], true);
    // One attempt costs about 6.3e-6 USD; the cap sits between one and two attempts.
    r.budget_usd = Some(0.00001);
    let report = r
        .run_cases(vec![case("c1")], &Thresholds::default())
        .await
        .unwrap();
    assert!(report.budget_exceeded, "{report:?}");
    assert!(!report.passed);
    assert!(report.total_cost_usd >= 0.00001);
}
