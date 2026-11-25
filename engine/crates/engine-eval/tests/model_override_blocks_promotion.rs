//! A comparison run with `EVAL_MODEL_OVERRIDE` is served by the override and its report can never
//! be promoted. This binary holds one test because it sets a process environment variable.
use async_trait::async_trait;
use engine_core::{Completion, LlmProvider, ProviderRouter, Route};
use engine_eval::{Case, Runner, Thresholds};
use std::sync::Arc;

struct Echo;

#[async_trait]
impl LlmProvider for Echo {
    fn name(&self) -> &'static str {
        "openrouter"
    }
    async fn complete(&self, _: &str, _: &str) -> anyhow::Result<Completion> {
        unreachable!()
    }
    async fn complete_route(&self, route: &Route, _: &str, _: u32) -> anyhow::Result<Completion> {
        Ok(Completion {
            text: "{}".into(),
            provider: "openrouter".into(),
            model: route.model.clone(),
            input_tokens: 10,
            output_tokens: 5,
            ..Completion::default()
        })
    }
    async fn stream(&self, _: &str, _: &str) -> anyhow::Result<engine_core::CompletionStream> {
        anyhow::bail!("not streaming")
    }
}

#[tokio::test]
async fn an_override_run_is_served_by_the_candidate_and_marked_unpromotable() {
    // SAFETY: the only test in this binary.
    unsafe { std::env::set_var("EVAL_MODEL_OVERRIDE", "preview=vendor/candidate>none") };
    let provider = Arc::new(Echo);
    let router = ProviderRouter::new(provider.clone(), provider).unwrap();
    let report = Runner::with_router(router, "live", false)
        .run_cases(
            vec![Case {
                id: "c".into(),
                task: "credit_envelope".into(),
                input: serde_json::json!({}),
                rubric: String::new(),
                checks: vec![],
            }],
            &Thresholds::default(),
        )
        .await
        .unwrap();
    assert_eq!(report.results[0].served_models, vec!["vendor/candidate"]);
    assert_eq!(report.status, "model_override");
    assert!(report.prompt_revision.contains("preview=vendor/candidate"));
    assert!(!report.live_promoted);
}
