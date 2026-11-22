//! E10-06 promotion gate in a deployed (live-router) engine.
//!
//! - Live router + passing evidence: the four safety screens call the live provider (an
//!   OpenRouter double here) and the golden must-block rows come back blocked; a malformed
//!   provider answer still fails closed.
//! - Live router + missing, stale or failed evidence: every gated RPC fails closed with
//!   UNAVAILABLE and a typed `google.rpc.ErrorInfo` reason, and the provider is never called.
//! - Fixture router: unchanged, whatever the evidence says.
use engine_core::{LlmProvider, ProviderRouter, promotion::Gate};
use engine_proto::{self as pb, engine_service_server::EngineService};
use engine_server::Service;
use serde_json::{Value, json};
use std::sync::Arc;
use tonic::{Code, Request};
use tonic_types::StatusExt;
use wiremock::{
    Mock, MockServer, Request as Received, Respond, ResponseTemplate, matchers::method,
};

const REV: &str = "b7979d57f81401aaabb21aba2cb82c64cddafd96";
const SHA: &str = "eecd8e63076797b780a1a5bb10ed83cc64ba249e788b8f149383dae4834cfb71";
const SCREENS: [&str; 4] = [
    "steer_screen",
    "character_screen",
    "course_screen",
    "screen_review",
];

fn golden(task: &str) -> Vec<Value> {
    include_str!("../../../../evals/datasets/duolify-golden/golden.jsonl")
        .lines()
        .filter(|l| !l.trim().is_empty())
        .map(|l| serde_json::from_str::<Value>(l).unwrap())
        .filter(|c| c["task"] == task)
        .collect()
}

/// A passing live report in the shape engine-eval writes, for the given build fingerprint.
fn report(fingerprint: &str, failing: &[&str]) -> String {
    let mut tasks = serde_json::Map::new();
    for t in engine_core::promotion::CANONICAL_TASKS
        .iter()
        .chain(SCREENS.iter())
    {
        let passed = !failing.contains(t);
        tasks.insert(
            (*t).into(),
            json!({"task": t, "passed": passed, "critical_failure": !passed, "total_cases": 2,
                   "passed_cases": if passed {2} else {1}, "score": if passed {1.0} else {0.5}, "threshold": 1.0}),
        );
    }
    json!({
        "dataset": "datamonsterr/duolify-golden", "dataset_revision": REV, "dataset_source": "repo",
        "dataset_verified": true, "dataset_sha256": SHA, "provider": "live", "status": "passed",
        "passed": failing.is_empty(), "critical_failure": !failing.is_empty(), "budget_exceeded": false,
        "fingerprint": fingerprint, "task_reports": tasks
    })
    .to_string()
}

/// OpenRouter double: answers each screen like the model is expected to, keyed on the golden
/// must-block phrases; `garbage` answers prose instead of the schema.
struct Model {
    garbage: bool,
}
impl Respond for Model {
    fn respond(&self, request: &Received) -> ResponseTemplate {
        let body = String::from_utf8_lossy(&request.body).to_lowercase();
        let has = |s: &str| body.contains(s);
        let content = if self.garbage {
            "Sure, this looks fine to me.".to_string()
        } else if has("steer_screen") {
            let reason = if has("graphic gore") {
                "unsafe_content"
            } else if has("love poem") {
                "off_topic_for_course"
            } else {
                ""
            };
            json!({"allowed": reason.is_empty(), "reason_code": reason}).to_string()
        } else if has("character_screen") {
            let reason = if has("taylor swift") {
                "real_person"
            } else {
                ""
            };
            json!({"approved": reason.is_empty(), "reason_code": reason}).to_string()
        } else {
            let codes: Vec<&str> = if has("ignore the screening rules") {
                vec!["prompt_injection", "personal_data"]
            } else if has("card number and password") {
                vec!["harmful_content"]
            } else {
                vec![]
            };
            let verdict = if codes.is_empty() { "pass" } else { "block" };
            json!({"verdict": verdict, "reason_codes": codes}).to_string()
        };
        ResponseTemplate::new(200).set_body_json(json!({
            "model": "tencent/hy4-flash",
            "choices": [{"message": {"content": content}, "finish_reason": "stop"}],
            "usage": {"prompt_tokens": 120, "completion_tokens": 12}
        }))
    }
}

async fn live(gate: Gate, garbage: bool) -> (Service, MockServer) {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .respond_with(Model { garbage })
        .mount(&server)
        .await;
    let p: Arc<dyn LlmProvider> = Arc::new(engine_providers::HttpProvider::openrouter(
        "test-key".into(),
        server.uri(),
    ));
    let svc = Service {
        router: Arc::new(ProviderRouter::new(p.clone(), p).unwrap()),
        speech: Arc::new(engine_core::narration::MockSpeechProvider),
        promotion: Arc::new(gate),
    };
    (svc, server)
}

fn limits() -> pb::UsageLimits {
    pb::UsageLimits {
        input_tokens: 18000,
        output_tokens: 200,
        timeout_ms: 10000,
        max_attempts: 2,
        max_cost_nanos: 50_000_000,
        input_nanos: 35,
        output_nanos: 141,
        cache_write_nanos: 35,
        cache_read_nanos: 2,
        deadline_unix_ms: std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_millis() as u64
            + 20_000,
        ..Default::default()
    }
}

/// Runs one golden screen case through its RPC; Ok(blocked) or the refusal status.
async fn screen(svc: &Service, task: &str, input: &Value) -> Result<bool, tonic::Status> {
    match task {
        "steer_screen" => svc
            .screen_instruction(Request::new(pb::ScreenInstructionRequest {
                text: input["text"].as_str().unwrap().into(),
                course_topic: input["topic"].as_str().unwrap().into(),
            }))
            .await
            .map(|r| !r.into_inner().allowed),
        "character_screen" => svc
            .screen_character(Request::new(pb::ScreenCharacterRequest {
                raw_prompt: input["prompt"].as_str().unwrap().into(),
                limits: Some(limits()),
                ..Default::default()
            }))
            .await
            .map(|r| !r.into_inner().approved),
        "course_screen" => svc
            .screen_course(Request::new(pb::ScreenCourseRequest {
                sample_json: input["sample"].to_string(),
                sampled_lessons: input["sample"]["lessons"].as_array().unwrap().len() as u32,
                limits: Some(pb::UsageLimits {
                    output_tokens: 300,
                    ..limits()
                }),
            }))
            .await
            .map(|r| r.into_inner().verdict != "pass"),
        "screen_review" => svc
            .screen_review(Request::new(pb::ScreenReviewRequest {
                text: input["text"].as_str().unwrap().into(),
                limits: Some(pb::UsageLimits {
                    input_tokens: 4000,
                    output_tokens: 300,
                    max_cost_nanos: 20_000_000,
                    ..limits()
                }),
            }))
            .await
            .map(|r| r.into_inner().verdict != "pass"),
        other => panic!("not a screen: {other}"),
    }
}

fn assert_typed(status: &tonic::Status, reason: &str, task: &str) {
    assert_eq!(status.code(), Code::Unavailable, "{status:?}");
    assert_eq!(status.message(), reason);
    let info = status
        .get_details_error_info()
        .expect("typed ErrorInfo detail");
    assert_eq!(info.reason, reason);
    assert_eq!(info.domain, engine_server::ERROR_DOMAIN);
    assert_eq!(info.metadata.get("task").map(String::as_str), Some(task));
}

#[tokio::test]
async fn passing_evidence_runs_screens_live_and_blocks_the_must_block_golden_rows() {
    let fp = engine_core::promotion::fingerprint();
    let (svc, server) = live(Gate::from_report(&report(&fp, &[]), &fp), false).await;
    let mut calls = 0;
    for task in SCREENS {
        let cases = golden(task);
        assert!(cases.len() >= 2, "{task} needs benign and must-block rows");
        for case in cases {
            let expected = case["input"]["expected_blocked"].as_bool().unwrap();
            let blocked = screen(&svc, task, &case["input"])
                .await
                .unwrap_or_else(|e| panic!("{}: {e:?}", case["id"]));
            assert_eq!(blocked, expected, "{}", case["id"]);
            calls += 1;
        }
    }
    // Every case reached the live provider, with the strict schema of ADR 0018.
    let requests = server.received_requests().await.unwrap();
    assert_eq!(requests.len(), calls);
    for r in &requests {
        let body: Value = serde_json::from_slice(&r.body).unwrap();
        assert_eq!(body["response_format"]["type"], "json_schema", "{body}");
    }
}

#[tokio::test]
async fn passing_evidence_still_fails_closed_on_a_malformed_live_answer() {
    let fp = engine_core::promotion::fingerprint();
    let (svc, _server) = live(Gate::from_report(&report(&fp, &[]), &fp), true).await;
    for task in SCREENS {
        for case in golden(task) {
            match screen(&svc, task, &case["input"]).await {
                Ok(blocked) => assert!(blocked, "{} must not pass on prose", case["id"]),
                Err(status) => assert_eq!(status.code(), Code::Unavailable, "{}", case["id"]),
            }
        }
    }
}

#[tokio::test]
async fn missing_stale_or_failed_evidence_fails_every_screen_closed_with_a_typed_reason() {
    let fp = engine_core::promotion::fingerprint();
    let gates = [
        (Gate::from_report("{}", &fp), "promotion_evidence_missing"),
        (
            Gate::from_report(&report("older-build", &[]), &fp),
            "promotion_evidence_stale",
        ),
        (
            Gate::from_report(&report(&fp, &["steer_screen"]), &fp),
            "promotion_evidence_rejected",
        ),
    ];
    for (gate, reason) in gates {
        let (svc, server) = live(gate, false).await;
        for task in SCREENS {
            for case in golden(task) {
                let status = screen(&svc, task, &case["input"]).await.unwrap_err();
                assert_typed(&status, reason, task);
            }
        }
        assert!(
            server.received_requests().await.unwrap().is_empty(),
            "{reason}: no provider call"
        );
    }
}

#[test]
fn launch_tasks_are_exactly_the_gated_rpcs_plus_live_narration() {
    let mut gated: Vec<&str> = engine_server::GATED.iter().map(|(_, t)| *t).collect();
    gated.push("lesson_narration");
    gated.sort_unstable();
    let mut launch = engine_core::promotion::LAUNCH_TASKS.to_vec();
    launch.sort_unstable();
    assert_eq!(gated, launch);
}

#[tokio::test]
async fn a_task_without_live_cases_stays_closed_even_when_the_run_passed() {
    let fp = engine_core::promotion::fingerprint();
    let (svc, server) = live(Gate::from_report(&report(&fp, &[]), &fp), false).await;
    let status = svc
        .map_character(Request::new(pb::MapCharacterRequest {
            prompt: "a friendly robot".into(),
            limits: Some(limits()),
        }))
        .await
        .unwrap_err();
    assert_typed(&status, "promotion_task_not_passed", "character_create");
    let status = svc
        .outline_chat_change(Request::new(pb::OutlineChatChangeRequest::default()))
        .await
        .unwrap_err();
    assert_typed(&status, "promotion_task_not_passed", "outline_chat_change");
    assert!(server.received_requests().await.unwrap().is_empty());
}

#[tokio::test]
async fn no_evidence_closes_learner_generation_rpcs_on_the_live_route() {
    let (svc, server) = live(Gate::from_report("{}", "x"), false).await;
    let preview = svc
        .preview_course(Request::new(pb::PreviewCourseRequest::default()))
        .await
        .unwrap_err();
    assert_typed(&preview, "promotion_evidence_missing", "course_preview");
    let chat = svc
        .chat(Request::new(pb::ChatRequest::default()))
        .await
        .err()
        .unwrap();
    assert_typed(&chat, "promotion_evidence_missing", "tutor_chat");
    let lesson = svc
        .generate_lesson(Request::new(pb::GenerateLessonRequest::default()))
        .await
        .err()
        .unwrap();
    assert_typed(&lesson, "promotion_evidence_missing", "lesson_slides");
    let setup = svc
        .setup_chat(Request::new(pb::SetupChatRequest::default()))
        .await
        .unwrap_err();
    assert_typed(&setup, "promotion_evidence_missing", "setup_chat");
    assert!(server.received_requests().await.unwrap().is_empty());
}

#[tokio::test]
async fn fixture_router_is_unchanged_without_evidence() {
    let svc = Service {
        router: Arc::new(engine_providers::router("mock").unwrap()),
        speech: Arc::new(engine_core::narration::MockSpeechProvider),
        promotion: Arc::new(Gate::from_report("{}", "x")),
    };
    let r = svc
        .screen_instruction(Request::new(pb::ScreenInstructionRequest {
            text: "More examples about pointers".into(),
            course_topic: "Go".into(),
        }))
        .await
        .unwrap()
        .into_inner();
    assert!(r.allowed);
}

#[tokio::test]
async fn live_narration_vendor_needs_narration_evidence() {
    struct Vendor;
    #[async_trait::async_trait]
    impl engine_core::narration::SpeechProvider for Vendor {
        fn name(&self) -> &'static str {
            "elevenlabs"
        }
        async fn synthesize(
            &self,
            _: &engine_core::narration::SpeechRequest,
        ) -> anyhow::Result<engine_core::narration::SpeechOutput> {
            panic!("a vendor call without evidence")
        }
    }
    let svc = Service {
        router: Arc::new(engine_providers::router("mock").unwrap()),
        speech: Arc::new(Vendor),
        promotion: Arc::new(Gate::from_report("{}", "x")),
    };
    let status = svc
        .synthesize_narration(Request::new(pb::SynthesizeNarrationRequest {
            text: "Hello".into(),
            language: "en".into(),
            voice_key: "default".into(),
            ..Default::default()
        }))
        .await
        .unwrap_err();
    assert_typed(&status, "promotion_evidence_missing", "lesson_narration");
}
