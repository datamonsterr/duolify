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
