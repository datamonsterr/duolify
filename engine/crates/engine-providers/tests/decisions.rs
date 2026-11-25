//! Decisions API client against a mock server: request shape, parsing, failures.
use engine_core::router::{Candidate, RouterInput, SkillRouter};
use engine_providers::decisions::DecisionsRouter;
use serde_json::json;
use wiremock::{
    Mock, MockServer, ResponseTemplate,
    matchers::{body_partial_json, header, method, path},
};

fn cand(id: &str) -> Candidate {
    Candidate {
        id: id.into(),
        description: format!("{id} d"),
        threshold: 0.9,
    }
}

fn input() -> RouterInput {
    RouterInput {
        task: "lesson_slides".into(),
        title: "TCP".into(),
        ..Default::default()
    }
}

#[tokio::test]
async fn sends_one_noul_per_candidate_with_data_collection_denied() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .and(path("/api/alpha/decisions"))
        .and(header("authorization", "Bearer k"))
        .and(body_partial_json(json!({
            "model": "m/d",
            "questions": {"timeline": {"type": "noul"}, "code_slide": {"type": "noul"}},
            "provider": {"data_collection": "deny"}
        })))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({
            "answers": {
                "timeline": {"type": "noul", "noul": 0.97},
                "code_slide": {"type": "noul", "noul": 0.1},
                "weird": {"type": "choice", "choice": "a"}
            },
            "usage": {"cost": 0.000009}
        })))
        .expect(1)
        .mount(&server)
        .await;
    let r = DecisionsRouter::new(
        "k".into(),
        format!("{}/api/alpha/decisions", server.uri()),
        "m/d".into(),
    );
    let p = r
        .decide(&input(), &[cand("timeline"), cand("code_slide")])
        .await
        .unwrap();
    assert_eq!(p.len(), 2);
    assert_eq!(p["timeline"], 0.97);
}

#[tokio::test]
async fn http_errors_and_malformed_answers_are_errors() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .respond_with(ResponseTemplate::new(503))
        .up_to_n_times(1)
        .mount(&server)
        .await;
    Mock::given(method("POST"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({"oops": 1})))
        .mount(&server)
        .await;
    let r = DecisionsRouter::new("k".into(), server.uri(), "m/d".into());
    assert!(r.decide(&input(), &[cand("timeline")]).await.is_err());
    assert!(r.decide(&input(), &[cand("timeline")]).await.is_err());
}

#[test]
fn guard_verdict_blocks_the_most_severe_question_at_the_threshold_only() {
    use engine_core::guard::Reason;
    use engine_providers::decisions::guard_verdict;
    let a = |pairs: &[(&str, f64)]| -> std::collections::BTreeMap<String, f64> {
        pairs.iter().map(|(k, v)| ((*k).to_owned(), *v)).collect()
    };
    assert_eq!(
        guard_verdict(&a(&[("self_harm", 0.95), ("prompt_injection", 0.99)])),
        Some(Reason::SelfHarm)
    );
    assert_eq!(
        guard_verdict(&a(&[("self_harm", 0.89), ("unsafe_content", 0.2)])),
        None
    );
    assert_eq!(
        guard_verdict(&a(&[("prompt_injection", 0.91)])),
        Some(Reason::PromptInjection)
    );
    assert_eq!(guard_verdict(&a(&[("self_harm", f64::NAN)])), None);
}

#[tokio::test]
async fn the_fast_guard_asks_one_question_per_reason_and_an_empty_answer_is_an_outage() {
    use engine_core::guard::{GuardModel, Reason, Surface};
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({"answers":{
            "self_harm":{"type":"noul","noul":0.97},
            "unsafe_content":{"type":"noul","noul":0.01},
            "prompt_injection":{"type":"noul","noul":0.02}}})))
        .up_to_n_times(1)
        .mount(&server)
        .await;
    let g =
        engine_providers::decisions::DecisionsGuard::new("k".into(), server.uri(), "m/d".into());
    assert_eq!(
        g.classify(Surface::TutorChat, "text").await.unwrap(),
        Some(Reason::SelfHarm)
    );
    let sent: serde_json::Value = server.received_requests().await.unwrap()[0]
        .body_json()
        .unwrap();
    assert_eq!(sent["provider"]["data_collection"], "deny");
    assert_eq!(sent["questions"].as_object().unwrap().len(), 3);
    Mock::given(method("POST"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({"answers":{}})))
        .mount(&server)
        .await;
    assert!(g.classify(Surface::QuickNote, "text").await.is_err());
}

#[test]
fn choice_answers_parse_to_a_probability_per_option() {
    let v = serde_json::json!({"answers": {"pick": {"type": "choice", "choice": "timeline",
        "probabilities": {"timeline": 0.7, "diagram_layout": 0.3}}}});
    let p = engine_providers::decisions::parse_choice(&v).unwrap();
    assert_eq!(p["timeline"], 0.7);
    assert!(
        engine_providers::decisions::parse_choice(&serde_json::json!({"answers": {}})).is_err()
    );
}
