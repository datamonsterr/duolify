//! OpenRouter-primary routing against a mock server: request body, fallback order,
//! served-model accounting and the optional direct Anthropic last resort.
use engine_core::{
    GatewayBudget, GatewayCacheStatus, GatewayReason, LlmProvider, OpenRouterTransport, Route,
    StreamEvent,
};
use engine_providers::{GatewayConfig, HttpProvider};
use futures::StreamExt;
use serde_json::{Value, json};
use std::time::Duration;
use wiremock::{
    Mock, MockServer, ResponseTemplate,
    matchers::{body_partial_json, header, method, path},
};

const HY4: &str = "tencent/hy4-flash";
const GLM: &str = "z-ai/glm-5.3-flash";
const GATEWAY_BASE_PATH: &str = "/v1/account/gateway/openrouter";
const GATEWAY_CHAT_PATH: &str = "/v1/account/gateway/openrouter/chat/completions";
const GATEWAY_AUDIO_PATH: &str = "/v1/account/gateway/openrouter/audio/transcriptions";
const DIRECT_AUDIO_PATH: &str = "/api/v1/audio/transcriptions";

fn gateway_case(name: &str) -> Value {
    let cases: Value = serde_json::from_str(include_str!(
        "../../../../evals/fixtures/openrouter-gateway.json"
    ))
    .unwrap();
    cases[name].clone()
}

fn gateway_provider(direct: &MockServer, gateway: &MockServer, allow_stt: bool) -> HttpProvider {
    let config = GatewayConfig::parse(
        Some(&format!("{}{GATEWAY_BASE_PATH}/", gateway.uri())),
        Some("gateway-token"),
        allow_stt.then_some("true"),
    )
    .unwrap();
    HttpProvider::openrouter_with_gateway(
        "openrouter-key".into(),
        format!("{}/api/v1/chat/completions", direct.uri()),
        config,
    )
    .unwrap()
}

#[tokio::test]
async fn gateway_chat_separates_credentials_and_caches_only_explicit_public_routes() {
    let gateway = MockServer::start().await;
    let direct = MockServer::start().await;
    let provider = gateway_provider(&direct, &gateway, false);
    let private_case = gateway_case("private");
    let public_case = gateway_case("public");
    let public_input = public_case["input"].as_str().unwrap();
    Mock::given(path(GATEWAY_CHAT_PATH))
        .respond_with(ok("response").insert_header("cf-aig-cache-status", "HIT"))
        .expect(4)
        .mount(&gateway)
        .await;
    let private = route(HY4, &[], &[]);
    let public = Route {
        gateway_cache_public: true,
        ..route(HY4, &[], &[])
    };
    let private_result = provider
        .complete_route(&private, private_case["input"].as_str().unwrap(), 200)
        .await
        .unwrap();
    let public_result = provider
        .complete_route(&public, public_input, 200)
        .await
        .unwrap();
    assert_eq!(
        private_result.transport,
        Some(OpenRouterTransport::Gateway(GatewayCacheStatus::Bypass))
    );
    assert_eq!(
        public_result.transport,
        Some(OpenRouterTransport::Gateway(GatewayCacheStatus::Hit))
    );
    provider
        .complete_route(&public, public_input, 200)
        .await
        .unwrap();
    provider
        .complete_route(&public, public_input, 201)
        .await
        .unwrap();
    let requests = gateway.received_requests().await.unwrap();
    for request in &requests {
        assert_eq!(request.headers["authorization"], "Bearer openrouter-key");
        assert_eq!(
            request.headers["cf-aig-authorization"],
            "Bearer gateway-token"
        );
        assert_eq!(request.headers["cf-aig-collect-log"], "false");
        assert_eq!(request.headers["cf-aig-max-attempts"], "1");
        assert_eq!(request.headers["cf-aig-no-wholesale"], "true");
    }
    assert_eq!(requests[0].headers["cf-aig-skip-cache"], "true");
    assert!(!requests[0].headers.contains_key("cf-aig-cache-ttl"));
    assert!(!requests[0].headers.contains_key("cf-aig-cache-key"));
    for request in &requests[1..] {
        assert!(!request.headers.contains_key("cf-aig-skip-cache"));
        assert_eq!(
            request.headers["cf-aig-cache-ttl"],
            public_case["expected_cache_ttl_seconds"].as_str().unwrap()
        );
        assert_eq!(request.headers["cf-aig-cache-key"].len(), 64);
    }
    assert_eq!(
        requests[1].headers["cf-aig-cache-key"],
        requests[2].headers["cf-aig-cache-key"]
    );
    assert_ne!(
        requests[2].headers["cf-aig-cache-key"],
        requests[3].headers["cf-aig-cache-key"]
    );
    assert!(direct.received_requests().await.unwrap().is_empty());
}

#[tokio::test]
async fn gateway_transcription_opt_in_retries_the_identical_zdr_payload_directly() {
    let gateway = MockServer::start().await;
    let direct = MockServer::start().await;
    let provider = gateway_provider(&direct, &gateway, true);
    let case = gateway_case("transcription");
    Mock::given(path(GATEWAY_AUDIO_PATH))
        .respond_with(
            ResponseTemplate::new(case["gateway_status"].as_u64().unwrap() as u16)
                .set_body_json(&case["gateway_body"]),
        )
        .mount(&gateway)
        .await;
    Mock::given(path(DIRECT_AUDIO_PATH))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({"text": "synthetic"})))
        .mount(&direct)
        .await;
    let route = Route {
        endpoint: Some(DIRECT_AUDIO_PATH.into()),
        zdr: true,
        data_collection: Some("deny".into()),
        ..Default::default()
    };
    let audio: Vec<u8> = serde_json::from_value(case["audio_bytes"].clone()).unwrap();
    let result = provider
        .transcribe(&route, "speech/model", &audio, "webm", Some("en"))
        .await
        .unwrap();
    assert_eq!(result.text, "synthetic");
    let via_gateway = gateway.received_requests().await.unwrap();
    let via_direct = direct.received_requests().await.unwrap();
    assert_eq!(via_gateway.len(), 1);
    assert_eq!(via_direct.len(), 1);
    let sent = &via_gateway[0];
    assert_eq!(sent.headers["cf-aig-collect-log"], "false");
    assert_eq!(sent.headers["cf-aig-skip-cache"], "true");
    assert!(!sent.headers.contains_key("cf-aig-cache-ttl"));
    let body: Value = sent.body_json().unwrap();
    assert_eq!(body["provider"], case["provider"]);
    assert_eq!(body["input_audio"], case["input_audio"]);
    assert_eq!(sent.body, via_direct[0].body);
    assert_eq!(
        via_direct[0].headers["authorization"],
        "Bearer openrouter-key"
    );
    assert!(
        via_direct[0]
            .headers
            .keys()
            .all(|name| !name.as_str().starts_with("cf-aig-"))
    );
}

#[tokio::test]
async fn gateway_timeout_uses_only_the_reserved_direct_retry() {
    let gateway = MockServer::start().await;
    let direct = MockServer::start().await;
    let provider = gateway_provider(&direct, &gateway, false);
    Mock::given(path(GATEWAY_CHAT_PATH))
        .respond_with(ok("slow").set_delay(Duration::from_secs(4)))
        .expect(2)
        .mount(&gateway)
        .await;
    Mock::given(path("/api/v1/chat/completions"))
        .respond_with(ok("direct"))
        .expect(1)
        .mount(&direct)
        .await;
    let mut route = Route {
        gateway_budget: Some(GatewayBudget {
            timeout: Duration::from_secs(1),
            may_retry_billable: true,
        }),
        ..route(HY4, &[], &[])
    };
    let result = provider.complete_route(&route, "test", 200).await.unwrap();
    assert_eq!(result.text, "direct");
    assert_eq!(
        result.transport,
        Some(OpenRouterTransport::Direct {
            fallback_reason: Some(GatewayReason::Timeout),
            gateway_may_bill: true
        })
    );
    route.gateway_budget.as_mut().unwrap().may_retry_billable = false;
    let error = provider
        .complete_route(&route, "test", 200)
        .await
        .unwrap_err();
    assert!(error.to_string().contains("timeout"));
}

#[tokio::test]
async fn gateway_rejects_upstream_client_errors_and_never_follows_redirects() {
    let gateway = MockServer::start().await;
    let direct = MockServer::start().await;
    let provider = gateway_provider(&direct, &gateway, false);
    for response in [
        ResponseTemplate::new(400).set_body_json(json!({"error": {"code": 400}})),
        ResponseTemplate::new(429).set_body_json(json!({"errors": [{"code": 2003}]})),
        ResponseTemplate::new(307).insert_header("location", direct.uri().as_str()),
    ] {
        gateway.reset().await;
        Mock::given(path(GATEWAY_CHAT_PATH))
            .respond_with(response)
            .expect(1)
            .mount(&gateway)
            .await;
        assert!(provider.complete(HY4, "private note").await.is_err());
        assert!(direct.received_requests().await.unwrap().is_empty());
    }
}

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
async fn null_content_keeps_its_measured_usage() {
    // Review F3/F8: a 200 with null content still consumed tokens. The attempt is returned as an
    // empty answer with its usage, so the bounded runner rejects and prices it, instead of an
    // error whose usage is unknown.
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({
            "model": HY4,
            "choices": [{"message": {"content": null}, "finish_reason": "length"}],
            "usage": {"prompt_tokens": 120, "completion_tokens": 2500}
        })))
        .mount(&server)
        .await;
    let provider = HttpProvider::openrouter("key".into(), server.uri());
    let c = provider
        .complete_route(&route(HY4, &[], &[]), "hello", 2500)
        .await
        .unwrap();
    assert_eq!(c.text, "");
    assert_eq!((c.input_tokens, c.output_tokens), (120, 2500));
    assert!(!c.usage_unavailable);
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

#[tokio::test]
async fn route_privacy_preferences_are_sent_to_openrouter() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({
            "model": HY4,
            "choices": [{"message": {"content": "answer"}, "finish_reason": "stop"}],
            "usage": {"prompt_tokens": 1, "completion_tokens": 1}
        })))
        .mount(&server)
        .await;
    let provider = HttpProvider::openrouter("key".into(), server.uri());
    let mut route = route(HY4, &[], &[]);
    route.zdr = true;
    route.data_collection = Some("deny".into());
    provider.complete_route(&route, "hello", 10).await.unwrap();
    let requests = server.received_requests().await.unwrap();
    let body: Value = requests[0].body_json().unwrap();
    assert_eq!(body["provider"]["zdr"], true);
    assert_eq!(body["provider"]["data_collection"], "deny");
}

fn audio_route(router: &engine_core::ProviderRouter, fallback: bool) -> Route {
    router.models.tasks["transcribe"].attempt_route(fallback)
}

#[tokio::test]
async fn transcription_posts_base64_audio_with_private_routing() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .and(wiremock::matchers::path("/api/v1/audio/transcriptions"))
        .and(header("authorization", "Bearer key"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({
            "text": "  hello world ",
            "language": "en",
            "usage": {"seconds": 4.2, "cost": 0.0000139, "total_tokens": 1}
        })))
        .mount(&server)
        .await;
    let router = engine_core::ProviderRouter::new(
        std::sync::Arc::new(engine_core::MockProvider),
        std::sync::Arc::new(engine_core::MockProvider),
    )
    .unwrap();
    let provider = HttpProvider::openrouter(
        "key".into(),
        format!("{}/api/v1/chat/completions", server.uri()),
    );
    let route = audio_route(&router, false);
    let t = provider
        .transcribe(&route, &route.model, &[1, 2, 3, 4], "webm", Some("en"))
        .await
        .unwrap();
    assert_eq!(t.text, "  hello world ");
    assert_eq!(t.language, "en");
    assert_eq!(t.seconds, 4.2);
    assert_eq!(t.cost_nanos, 13_900);
    assert_eq!(t.model, "qwen/qwen3-asr-0.6b");

    let requests = server.received_requests().await.unwrap();
    let body: Value = requests[0].body_json().unwrap();
    assert_eq!(body["model"], "qwen/qwen3-asr-0.6b");
    assert_eq!(body["input_audio"]["format"], "webm");
    assert_eq!(body["input_audio"]["data"], "AQIDBA==");
    assert_eq!(body["language"], "en");
    assert_eq!(body["provider"]["data_collection"], "deny");
    assert_eq!(body["provider"]["zdr"], true);
}

#[tokio::test]
async fn transcription_failures_carry_no_response_body() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .respond_with(
            ResponseTemplate::new(404).set_body_string("no zdr endpoints for SECRET-TEXT"),
        )
        .mount(&server)
        .await;
    let router = engine_core::ProviderRouter::new(
        std::sync::Arc::new(engine_core::MockProvider),
        std::sync::Arc::new(engine_core::MockProvider),
    )
    .unwrap();
    let provider = HttpProvider::openrouter("key".into(), server.uri());
    let route = audio_route(&router, true);
    let err = provider
        .transcribe(&route, &route.model, &[9], "m4a", None)
        .await
        .unwrap_err();
    let message = format!("{err:#}");
    assert!(message.contains("404"), "{message}");
    assert!(!message.contains("SECRET-TEXT"));
    let sent: Value = server.received_requests().await.unwrap()[0]
        .body_json()
        .unwrap();
    assert!(sent.get("language").is_none());
    assert_eq!(sent["model"], "openai/whisper-large-v3-turbo");
}

#[tokio::test]
async fn unknown_language_values_are_dropped() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({
            "text": "x", "language": "english (us)", "usage": {}
        })))
        .mount(&server)
        .await;
    let router = engine_core::ProviderRouter::new(
        std::sync::Arc::new(engine_core::MockProvider),
        std::sync::Arc::new(engine_core::MockProvider),
    )
    .unwrap();
    let provider = HttpProvider::openrouter("key".into(), server.uri());
    let route = audio_route(&router, false);
    let t = provider
        .transcribe(&route, &route.model, &[1], "webm", None)
        .await
        .unwrap();
    assert_eq!(t.language, "");
    assert_eq!(t.cost_nanos, 0);
}
