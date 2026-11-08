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
