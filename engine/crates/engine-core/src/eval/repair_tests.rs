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
        Shape {
            format: Some(crate::structured::lesson_format()),
            prepare: None,
        },
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
fn same_order_variant_is_rotated_and_then_validates() {
    let q = |id: &str, extra: serde_json::Value| {
        let mut v = serde_json::json!({"question_id": id, "topic_id": "t",
            "prompt": format!("Which colour {id}?"), "choices": ["Red", "Blue", "Green"],
            "answer_index": 1, "explanation": "Blue is right."});
        v.as_object_mut()
            .unwrap()
            .extend(extra.as_object().unwrap().clone());
        v
    };
    let lesson = serde_json::json!({
        "slides": [{"html": "<div id=\"root\">Colours</div>", "narration": "Colours.",
                    "duration_ms": 3000, "topic_ids": ["t"]}],
        "test": [q("q1", serde_json::json!({}))],
        "variant_pool": [{"topic_id": "t", "variants": [q("q1-v1", serde_json::json!({"variant_of": "q1"}))]}]
    })
    .to_string();
    assert!(
        crate::lessontest::check_generated(&lesson, &[], "")
            .unwrap_err()
            .contains("order must differ")
    );
    let fixed = crate::lessontest::prepare_lesson(&lesson).unwrap();
    crate::lessontest::check_generated(&fixed, &[], "").unwrap();
    let v: serde_json::Value = serde_json::from_str(&fixed).unwrap();
    let variant = &v["variant_pool"][0]["variants"][0];
    let choices = variant["choices"].as_array().unwrap();
    assert_eq!(
        choices[variant["answer_index"].as_u64().unwrap() as usize],
        "Blue"
    );
    // A lesson that needs no fix is left alone.
    assert!(crate::lessontest::prepare_lesson(&fixed).is_none());
    // A reordered variant that marks the wrong choice points back at the source's correct text.
    let mut wrong: serde_json::Value = serde_json::from_str(&fixed).unwrap();
    wrong["variant_pool"][0]["variants"][0]["choices"] =
        serde_json::json!(["Green", "Red", "Blue"]);
    wrong["variant_pool"][0]["variants"][0]["answer_index"] = serde_json::json!(0);
    let wrong = wrong.to_string();
    assert!(crate::lessontest::check_generated(&wrong, &[], "").is_err());
    let fixed = crate::lessontest::prepare_lesson(&wrong).unwrap();
    crate::lessontest::check_generated(&fixed, &[], "").unwrap();
    let v: serde_json::Value = serde_json::from_str(&fixed).unwrap();
    assert_eq!(v["variant_pool"][0]["variants"][0]["answer_index"], 2);
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

/// The longer contracts must still fit the approved quote envelopes (input bytes), with room
/// left for learner data: preview 2000, lesson 8000 (pricing fixture `usd_design_chat_owner_approved`).
#[test]
fn trusted_prompt_text_leaves_room_in_the_approved_envelopes() {
    let brief = crate::courses::Brief {
        topic: "Go programming".into(),
        goal: "build small command line tools".into(),
        level: "beginner".into(),
        ..Default::default()
    };
    let profile = serde_json::json!({"locale":"en","background":"","goals":"","level":"beginner",
        "topic":"","daily_minutes":10,"daily_xp":20,"time_zone":"UTC","reminder_enabled":false,
        "reminder_time":""});
    let preview = crate::courses::preview_prompt(&brief, profile);
    assert!(
        preview.len() <= 1500,
        "preview prompt {} bytes",
        preview.len()
    );
    // A 12-lesson course outline as context, wrapped in the steering envelope as production does.
    let context = "x".repeat(2500);
    let lesson =
        crate::steering::hardened_prompt(&crate::lessontest::lesson_prompt(&context), &[]).unwrap();
    assert!(lesson.len() <= 5500, "lesson prompt {} bytes", lesson.len());
}
