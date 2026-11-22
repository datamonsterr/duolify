//! OpenRouter-primary routing against a mock server: request body, fallback order,
//! served-model accounting and the optional direct Anthropic last resort.
use engine_core::{LlmProvider, Route, StreamEvent};
use engine_providers::HttpProvider;
use futures::StreamExt;
use serde_json::{Value, json};
use wiremock::{
    Mock, MockServer, ResponseTemplate,
    matchers::{body_partial_json, header, method},
};

const HY4: &str = "tencent/hy4-flash";
const GLM: &str = "z-ai/glm-5.3-flash";

fn route(model: &str, fallbacks: &[&str], efforts: &[(&str, &str)]) -> Route {
    Route {
        model: model.into(),
        fallbacks: fallbacks.iter().map(|m| (*m).into()).collect(),
        reasoning: efforts
            .iter()
            .map(|(m, e)| ((*m).into(), (*e).into()))
            .collect(),
        ..Route::default()
    }
}

fn ok(content: &str) -> ResponseTemplate {
    ResponseTemplate::new(200).set_body_json(json!({
        "model": HY4,
        "choices": [{"message": {"content": content}}],
        "usage": {"prompt_tokens": 1, "completion_tokens": 2}
    }))
}

#[tokio::test]
async fn structured_task_sends_its_schema_and_requires_supporting_endpoints() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .respond_with(ok("{}"))
        .mount(&server)
        .await;
    let router = engine_core::ProviderRouter::new(
        std::sync::Arc::new(engine_core::MockProvider),
        std::sync::Arc::new(engine_core::MockProvider),
    )
    .unwrap();
    let provider = HttpProvider::openrouter("key".into(), server.uri());
    // Every screen, on both chain members, with the effort configured per model.
    for task in [
        "steer_screen",
        "character_screen",
        "course_screen",
        "screen_review",
    ] {
        let route = router.models.tasks[task].primary_route();
        provider.complete_route(&route, "{}", 200).await.unwrap();
    }
    let requests = server.received_requests().await.unwrap();
    assert_eq!(requests.len(), 4);
    for r in &requests {
        let body: Value = r.body_json().unwrap();
        assert_eq!(body["model"], HY4);
        assert_eq!(
            body["reasoning"]["effort"], "none",
            "screens answer without reasoning"
        );
        assert_eq!(body["response_format"]["type"], "json_schema");
        assert_eq!(body["response_format"]["json_schema"]["strict"], true);
        assert_eq!(body["provider"]["require_parameters"], true);
        assert_eq!(body["provider"]["data_collection"], "deny");
    }
}

#[tokio::test]
async fn fallback_member_keeps_the_schema_and_its_own_effort() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .and(body_partial_json(json!({"model": HY4})))
        .respond_with(ResponseTemplate::new(503))
        .mount(&server)
        .await;
    Mock::given(method("POST"))
        .and(body_partial_json(json!({"model": GLM})))
        .respond_with(ok("{}"))
        .mount(&server)
        .await;
    let router = engine_core::ProviderRouter::new(
        std::sync::Arc::new(engine_core::MockProvider),
        std::sync::Arc::new(engine_core::MockProvider),
    )
    .unwrap();
    let provider = HttpProvider::openrouter("key".into(), server.uri());
    let route = router.models.tasks["course_screen"].primary_route();
    provider.complete_route(&route, "{}", 300).await.unwrap();
    let glm: Value = server.received_requests().await.unwrap()[1]
        .body_json()
        .unwrap();
    assert_eq!(glm["model"], GLM);
    assert_eq!(glm["reasoning"]["effort"], "low");
    assert_eq!(
        glm["response_format"]["json_schema"]["name"],
        "course_screen_verdict"
    );
    assert_eq!(glm["provider"]["require_parameters"], true);
}

#[tokio::test]
async fn prose_tasks_send_no_response_format() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .respond_with(ok("hi"))
        .mount(&server)
        .await;
    let provider = HttpProvider::openrouter("key".into(), server.uri());
    provider
        .complete_route(&route(HY4, &[], &[]), "hello", 10)
        .await
        .unwrap();
    let body = first_body(&server).await;
    assert!(body.get("response_format").is_none());
    assert!(body["provider"].get("require_parameters").is_none());
}

#[tokio::test]
async fn repair_attempt_replays_the_rejected_answer_and_the_correction() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .respond_with(ok("{}"))
        .mount(&server)
        .await;
    let provider = HttpProvider::openrouter("key".into(), server.uri());
    let mut r = route(GLM, &[], &[]);
    r.repair = Some(engine_core::Repair {
        previous: "**verdict:** pass".into(),
        message: "Your previous answer was rejected: not JSON.".into(),
    });
    provider
        .complete_route(&r, "screen this", 10)
        .await
        .unwrap();
    let body = first_body(&server).await;
    assert_eq!(
        body["messages"],
        json!([
            {"role": "user", "content": "screen this"},
            {"role": "assistant", "content": "**verdict:** pass"},
            {"role": "user", "content": "Your previous answer was rejected: not JSON."}
        ])
    );
}

async fn first_body(server: &MockServer) -> Value {
    server.received_requests().await.unwrap()[0]
        .body_json()
        .unwrap()
}

#[tokio::test]
async fn sends_reasoning_privacy_and_attribution_headers() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .and(header("authorization", "Bearer key"))
        .and(header("http-referer", "https://duolify.online"))
        .and(header("x-title", "Duolify"))
        .and(body_partial_json(json!({
            "model": HY4,
            "reasoning": {"effort": "none"},
            "provider": {"data_collection": "deny"},
            "max_tokens": 77,
        })))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({
            "model": HY4,
            "choices": [{"message": {"content": "answer"}}],
            "usage": {"prompt_tokens": 40, "completion_tokens": 10,
                      "prompt_tokens_details": {"cached_tokens": 15}}
        })))
        .expect(1)
        .mount(&server)
        .await;
    let provider = HttpProvider::openrouter("key".into(), server.uri());
    let r = route(HY4, &[GLM], &[(HY4, "none"), (GLM, "low")]);
    let c = provider.complete_route(&r, "hello", 77).await.unwrap();
    assert_eq!(c.model, HY4);
    assert_eq!(c.provider, "openrouter");
    assert_eq!((c.input_tokens, c.cache_read_tokens), (25, 15));
    let body = first_body(&server).await;
    assert!(
        body.get("models").is_none(),
        "no request-level models array"
    );
}

#[tokio::test]
async fn chain_falls_back_in_order_with_per_model_effort_and_reports_served_model() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .and(body_partial_json(json!({"model": HY4})))
        .respond_with(ResponseTemplate::new(503))
        .expect(1)
        .mount(&server)
        .await;
    Mock::given(method("POST"))
        .and(body_partial_json(
            json!({"model": GLM, "reasoning": {"effort": "low"}}),
        ))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({
            "model": GLM,
            "choices": [{"message": {"content": "from-glm"}}],
            "usage": {"prompt_tokens": 3, "completion_tokens": 4}
        })))
        .expect(1)
        .mount(&server)
        .await;
    let provider = HttpProvider::openrouter("key".into(), server.uri());
    let r = route(HY4, &[GLM], &[(HY4, "none"), (GLM, "low")]);
    let c = provider.complete_route(&r, "hello", 10).await.unwrap();
    // The model that served the call is what cost accounting sees.
    assert_eq!((c.text.as_str(), c.model.as_str()), ("from-glm", GLM));
    let requests = server.received_requests().await.unwrap();
    assert_eq!(requests.len(), 2);
    let first: Value = requests[0].body_json().unwrap();
    assert_eq!(first["reasoning"]["effort"], "none");
}

#[tokio::test]
async fn whole_chain_failure_returns_an_error() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .respond_with(ResponseTemplate::new(500))
        .expect(2)
        .mount(&server)
        .await;
    let provider = HttpProvider::openrouter("key".into(), server.uri());
    assert!(
        provider
            .complete_route(&route(HY4, &[GLM], &[]), "hello", 10)
            .await
            .is_err()
    );
}

#[tokio::test]
async fn response_without_model_reports_the_requested_id() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({
            "choices": [{"message": {"content": "answer"}}],
            "usage": {"prompt_tokens": 1, "completion_tokens": 2}
        })))
        .mount(&server)
        .await;
    let provider = HttpProvider::openrouter("key".into(), server.uri());
    let c = provider
        .complete_route(&route(HY4, &[], &[]), "hello", 10)
        .await
        .unwrap();
    assert_eq!(c.model, HY4);
    assert!(first_body(&server).await.get("reasoning").is_none());
}

#[tokio::test]
async fn reasoning_tokens_bill_as_output() {
    for (completion, reasoning, billed) in [(100, 60, 100), (20, 50, 70)] {
        let server = MockServer::start().await;
        Mock::given(method("POST"))
            .respond_with(ResponseTemplate::new(200).set_body_json(json!({
                "choices": [{"message": {"content": "answer"}}],
                "usage": {"prompt_tokens": 1, "completion_tokens": completion,
                          "completion_tokens_details": {"reasoning_tokens": reasoning}}
            })))
            .mount(&server)
            .await;
        let provider = HttpProvider::openrouter("key".into(), server.uri());
        let c = provider
            .complete_route(&route("m", &[], &[("m", "low")]), "hello", 10)
            .await
            .unwrap();
        assert_eq!(c.output_tokens, billed);
    }
}

#[tokio::test]
async fn stream_reports_served_model_and_reasoning_output() {
    let server = MockServer::start().await;
    let events = format!(
        "data: {}\n\ndata: {}\n\ndata: [DONE]\n\n",
        json!({"model": HY4, "choices": [{"delta": {"content": "hi"}}]}),
        json!({"model": HY4, "usage": {"prompt_tokens": 9, "completion_tokens": 5,
               "completion_tokens_details": {"reasoning_tokens": 30}}}),
    );
    Mock::given(method("POST"))
        .and(body_partial_json(json!({"model": "a", "stream": true})))
        .respond_with(ResponseTemplate::new(200).set_body_string(events))
        .mount(&server)
        .await;
    let provider = HttpProvider::openrouter("key".into(), server.uri());
    let mut stream = provider
        .stream_route(&route("a", &[], &[("a", "low")]), "hello")
        .await
        .unwrap();
    let mut usage = None;
    while let Some(event) = stream.next().await {
        if let StreamEvent::Usage(u) = event.unwrap() {
            usage = Some(u);
        }
    }
    let usage = usage.unwrap();
    assert_eq!(usage.model, HY4);
    assert_eq!(usage.output_tokens, 35);
}
