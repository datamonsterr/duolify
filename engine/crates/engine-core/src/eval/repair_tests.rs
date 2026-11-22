//! Structured extraction and the corrective repair inside the bounded runner.
use super::*;
use crate::{Completion, LlmProvider, ProviderRouter, Route};
use async_trait::async_trait;
use std::{
    collections::VecDeque,
    sync::{Arc, Mutex},
};

/// Replies in order and records every route it was asked to serve.
#[derive(Default)]
struct Scripted {
    replies: Mutex<VecDeque<&'static str>>,
    routes: Mutex<Vec<Route>>,
}

#[async_trait]
impl LlmProvider for Scripted {
    fn name(&self) -> &'static str {
        "scripted"
    }
    async fn complete(&self, _: &str, _: &str) -> anyhow::Result<Completion> {
        unreachable!("routed calls only")
    }
    async fn complete_route(&self, route: &Route, _: &str, _: u32) -> anyhow::Result<Completion> {
        self.routes.lock().unwrap().push(route.clone());
        let text = self
            .replies
            .lock()
            .unwrap()
            .pop_front()
            .context("no reply")?;
        Ok(Completion {
            text: text.into(),
            provider: "scripted".into(),
            model: route.model.clone(),
            input_tokens: 10,
            output_tokens: 5,
            ..Completion::default()
        })
    }
    async fn stream(&self, _: &str, _: &str) -> anyhow::Result<crate::CompletionStream> {
        anyhow::bail!("not streaming")
    }
}

fn router(replies: &[&'static str]) -> (ProviderRouter, Arc<Scripted>) {
    let p = Arc::new(Scripted {
        replies: Mutex::new(replies.iter().copied().collect()),
        ..Scripted::default()
    });
    (ProviderRouter::new(p.clone(), p.clone()).unwrap(), p)
}

fn limits() -> Limits {
    Limits {
        input_tokens: 4000,
        output_tokens: 300,
        timeout_ms: 10000,
        max_attempts: 2,
        max_cost_nanos: 20_000_000,
        input_nanos: 1000,
        output_nanos: 5000,
        cache_write_nanos: 1250,
        cache_read_nanos: 100,
        deadline_unix_ms: 0,
    }
}

async fn review(replies: &[&'static str]) -> (crate::credits::Outcome, Arc<Scripted>) {
    let (router, p) = router(replies);
    let o = crate::sharing::screen_review(&router, "Clear examples, thanks.", &limits())
        .await
        .unwrap();
    (o, p)
}

#[tokio::test]
async fn fenced_answer_is_delivered_as_the_extracted_json() {
    let (o, p) = review(&["```json\n{\"verdict\":\"pass\",\"reason_codes\":[]}\n```"]).await;
    let c = o.completion.unwrap();
    assert_eq!(c.text, r#"{"verdict":"pass","reason_codes":[]}"#);
    assert_eq!(o.attempts.len(), 1);
    // The request carried the task's strict schema.
    let route = &p.routes.lock().unwrap()[0];
    assert_eq!(
        route.response_format.as_ref().unwrap()["json_schema"]["name"],
        "review_screen_verdict"
    );
    assert!(route.repair.is_none());
}

#[tokio::test]
async fn prose_verdict_is_rejected_and_the_repair_quotes_the_error() {
    let (o, p) = review(&[
        "**verdict:** pass",
        r#"{"verdict":"pass","reason_codes":[]}"#,
    ])
    .await;
    assert!(o.completion.is_some());
    assert_eq!(o.attempts.len(), 2);
    assert_eq!(o.attempts[0].error_code.as_deref(), Some("invalid_schema"));
    let routes = p.routes.lock().unwrap();
    let repair = routes[1].repair.as_ref().unwrap();
    assert_eq!(repair.previous, "**verdict:** pass");
    assert!(
        repair.message.contains("not a JSON object"),
        "{}",
        repair.message
    );
}

#[tokio::test]
async fn unknown_reason_code_is_quoted_not_mapped() {
    let (o, p) = review(&[
        r#"{"verdict":"block","reason_codes":["secrets"]}"#,
        r#"{"verdict":"block","reason_codes":["secret"]}"#,
    ])
    .await;
    let codes = crate::sharing::verdict(&o.completion.unwrap().text)
        .unwrap()
        .1;
    assert_eq!(codes, ["secret"]);
    let message = p.routes.lock().unwrap()[1].repair.clone().unwrap().message;
    assert!(message.contains("\"secrets\" is not one of"), "{message}");
}

#[tokio::test]
async fn two_invalid_answers_fail_closed_without_a_third_call() {
    let (o, p) = review(&["pass", "block phishing,personal_data", "unused"]).await;
    assert!(o.completion.is_none(), "no verdict is ever read from prose");
    assert_eq!(o.attempts.len(), 2);
    assert_eq!(p.routes.lock().unwrap().len(), 2);
}

#[tokio::test]
async fn truncated_json_is_invalid() {
    let (o, _) = review(&[
        r#"{"verdict":"pa"#,
        r#"{"verdict":"block","reason_codes":[]"#,
    ])
    .await;
    assert!(o.completion.is_none());
}

#[tokio::test]
async fn repair_is_dropped_when_it_would_exceed_the_input_envelope() {
    let (router, p) = router(&["not json", r#"{"allowed":true,"reason_code":""}"#]);
    let mut l = limits();
    let input = "x".repeat(3990);
    l.input_tokens = 4000;
    let o = crate::credits::run_validated_checked(&router, "steer_screen", &input, &l, |raw| {
        crate::steering::check_verdict(raw).map(drop)
    })
    .await
    .unwrap();
    assert!(o.completion.is_some());
    assert!(p.routes.lock().unwrap()[1].repair.is_none());
}

#[tokio::test]
async fn single_attempt_quotes_have_no_repair() {
    let (router, p) = router(&["nope"]);
    let mut l = limits();
    l.max_attempts = 1;
    let o = crate::credits::run_validated(&router, "note", "{}", &l, |_| false)
        .await
        .unwrap();
    assert!(o.completion.is_none());
    assert_eq!(p.routes.lock().unwrap().len(), 1);
}

#[test]
fn screen_validators_explain_each_rejection() {
    use crate::{characters, sharing, steering};
    assert!(
        steering::check_verdict(r#"{"allowed":true,"reason_code":"prompt_injection"}"#)
            .unwrap_err()
            .contains("must be \"\"")
    );
    assert!(
        steering::check_verdict(r#"{"allowed":false,"reason_code":"spam"}"#)
            .unwrap_err()
            .contains("\"spam\" is not one of")
    );
    assert!(
        characters::check_verdict(r#"{"approved":false,"reason_code":"celebrity"}"#)
            .unwrap_err()
            .contains("real_person")
    );
    assert!(
        sharing::check_verdict(r#"{"verdict":"block","reason_codes":[]}"#)
            .unwrap_err()
            .contains("one to four")
    );
    assert!(
        sharing::check_verdict(r#"{"verdict":"allowed","reason_codes":[]}"#)
            .unwrap_err()
            .contains("pass, needs_review, block")
    );
    assert_eq!(
        sharing::check_verdict(
            r#"{"verdict":"block","reason_codes":["harmful_content","personal_data"]}"#
        )
        .unwrap()
        .1,
        ["harmful_content", "personal_data"]
    );
}

#[tokio::test]
async fn lesson_generation_overrides_the_task_format_with_the_lesson_schema() {
    let (formatted, p) = router(&["{}"]);
    let _ = run_bounded_formatted(
        &formatted,
        "lesson",
        &["{}"],
        &limits(),
        Some(crate::structured::lesson_format()),
        |_| Ok(()),
        true,
    )
    .await
    .unwrap();
    let f = p.routes.lock().unwrap()[0].response_format.clone().unwrap();
    assert_eq!(f["json_schema"]["name"], "lesson");
    assert_eq!(f["json_schema"]["strict"], true);
    assert_eq!(
        f["json_schema"]["schema"]["properties"]["test"]["minItems"],
        1
    );
    // Without an override the shared `lesson` route keeps JSON mode (retry variants use it).
    let (plain, p) = router(&["{}"]);
    let _ = run_bounded_checked(&plain, "lesson", &["{}"], &limits(), |_| Ok(()), true)
        .await
        .unwrap();
    let f = p.routes.lock().unwrap()[0].response_format.clone().unwrap();
    assert_eq!(f["type"], "json_object");
}

#[test]
fn lesson_rejection_names_the_offending_slide_text() {
    let lesson = serde_json::json!({
        "slides": [{"html": "<div id=\"root\">a&nbsp;b</div>", "narration": "A view.",
                    "duration_ms": 3000, "topic_ids": ["t"]}],
        "test": [{"question_id": "q", "topic_id": "t", "prompt": "?", "choices": ["a", "b"],
                  "answer_index": 0, "explanation": "e"}]
    });
    let e = crate::lessontest::check_generated(&lesson.to_string(), &[], "").unwrap_err();
    assert!(e.contains("found \"&nbsp;\""), "{e}");
}
