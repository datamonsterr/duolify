use engine_core::LlmProvider;
use engine_providers::HttpProvider;
use wiremock::{
    Mock, MockServer, ResponseTemplate,
    matchers::{body_partial_json, header, method, path},
};
#[tokio::test]
async fn anthropic_request_and_response() {
    let server = MockServer::start().await;
    Mock::given(method("POST")).and(path("/v1/messages")).and(header("x-api-key", "key")).and(body_partial_json(serde_json::json!({"model":"m","max_tokens":4096,"messages":[{"role":"user","content":"hello"}]}))).respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({"content":[{"type":"text","text":"answer"}],"usage":{"input_tokens":2,"output_tokens":3}}))).mount(&server).await;
    let p = HttpProvider::anthropic("key".into(), format!("{}/v1/messages", server.uri()));
    let c = p.complete("m", "hello").await.unwrap();
    assert_eq!(c.text, "answer");
    assert_eq!(c.output_tokens, 3);
}
#[tokio::test]
async fn openrouter_request_and_response() {
    let server = MockServer::start().await;
    Mock::given(method("POST")).and(header("authorization", "Bearer key")).and(body_partial_json(serde_json::json!({"model":"m","stream":false}))).respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({"choices":[{"message":{"content":"answer"}}],"usage":{"prompt_tokens":4,"completion_tokens":5}}))).mount(&server).await;
    let p = HttpProvider::openrouter("key".into(), server.uri());
    let c = p.complete("m", "hello").await.unwrap();
    assert_eq!(c.text, "answer");
    assert_eq!(c.input_tokens, 4);
}

#[tokio::test]
async fn missing_and_partial_usage_are_distinct_from_measured_zero() {
    for anthropic in [false, true] {
        for usage in [
            serde_json::Value::Null,
            serde_json::json!({}),
            serde_json::json!({"input_tokens":0,"prompt_tokens":0}),
            serde_json::json!({"input_tokens":0,"output_tokens":0,"prompt_tokens":0,"completion_tokens":0}),
        ] {
            let server = MockServer::start().await;
            let body = if anthropic {
                serde_json::json!({"content":[{"text":"answer"}],"usage":usage})
            } else {
                serde_json::json!({"choices":[{"message":{"content":"answer"}}],"usage":usage})
            };
            Mock::given(method("POST"))
                .respond_with(ResponseTemplate::new(200).set_body_json(body))
                .mount(&server)
                .await;
            let provider = if anthropic {
                HttpProvider::anthropic("test".into(), server.uri())
            } else {
                HttpProvider::openrouter("test".into(), server.uri())
            };
            let completion = provider.complete("m", "input").await.unwrap();
            let expected = usage.get("output_tokens").is_none();
            assert_eq!(
                serde_json::to_value(completion).unwrap()["usage_unavailable"],
                serde_json::json!(expected),
                "anthropic={anthropic} usage={usage}"
            );
        }
    }
}

#[tokio::test]
async fn streaming_usage_requires_both_input_and_output_measurements() {
    use futures::StreamExt;
    for anthropic in [false, true] {
        for measured in [false, true] {
            let server = MockServer::start().await;
            let events = match (anthropic, measured) {
                (true, false) => "data: {\"type\":\"message_stop\"}\n\n",
                (true, true) => {
                    "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":0}}}\n\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":0}}\n\ndata: {\"type\":\"message_stop\"}\n\n"
                }
                (false, false) => "data: [DONE]\n\n",
                (false, true) => {
                    "data: {\"usage\":{\"prompt_tokens\":0,\"completion_tokens\":0}}\n\ndata: [DONE]\n\n"
                }
            };
            Mock::given(method("POST"))
                .respond_with(ResponseTemplate::new(200).set_body_string(events))
                .mount(&server)
                .await;
            let provider = if anthropic {
                HttpProvider::anthropic("test".into(), server.uri())
            } else {
                HttpProvider::openrouter("test".into(), server.uri())
            };
            let mut stream = provider.stream("m", "input").await.unwrap();
            let engine_core::StreamEvent::Usage(completion) = stream.next().await.unwrap().unwrap()
            else {
                panic!("missing usage event")
            };
            assert_eq!(
                serde_json::to_value(completion).unwrap()["usage_unavailable"],
                serde_json::json!(!measured)
            );
        }
    }
}

#[tokio::test]
async fn parses_anthropic_stream() {
    use futures::StreamExt;
    let server = MockServer::start().await;
    Mock::given(method("POST")).and(body_partial_json(serde_json::json!({"stream":true})))
        .respond_with(ResponseTemplate::new(200).set_body_string("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n"))
        .mount(&server).await;
    let p = HttpProvider::anthropic("key".into(), server.uri());
    let mut stream = p.stream("m", "input").await.unwrap();
    assert!(
        matches!(stream.next().await.unwrap().unwrap(), engine_core::StreamEvent::Delta(s) if s == "hello")
    );
    assert!(matches!(
        stream.next().await.unwrap().unwrap(),
        engine_core::StreamEvent::Usage(_)
    ));
    assert!(stream.next().await.is_none());
}

#[tokio::test]
async fn stream_retains_cache_classes() {
    use futures::StreamExt;
    let server = MockServer::start().await;
    let events = "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":4,\"cache_creation_input_tokens\":6,\"cache_read_input_tokens\":9}}}\n\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":5}}\n\ndata: {\"type\":\"message_stop\"}\n\n";
    Mock::given(method("POST"))
        .respond_with(ResponseTemplate::new(200).set_body_string(events))
        .mount(&server)
        .await;
    let provider = HttpProvider::anthropic("test".into(), server.uri());
    let mut stream = provider.stream("m", "input").await.unwrap();
    let engine_core::StreamEvent::Usage(usage) = stream.next().await.unwrap().unwrap() else {
        panic!("missing usage")
    };
    assert_eq!(
        (
            usage.input_tokens,
            usage.output_tokens,
            usage.cache_write_tokens,
            usage.cache_read_tokens
        ),
        (4, 5, 6, 9)
    );
}
