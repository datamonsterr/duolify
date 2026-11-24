use engine_core::narration::{SpeechProvider, SpeechRequest, VendorFailure, validate};
use engine_providers::openrouter_tts::{Model, OpenRouterSpeech};
use wiremock::matchers::{body_json, header, method, path};
use wiremock::{Mock, MockServer, ResponseTemplate};

const MP3: &[u8] = include_bytes!("../../../../evals/fixtures/narration.openrouter.mp3");

fn request() -> SpeechRequest {
    SpeechRequest {
        text: "¡Mira, qué bonita!".into(),
        language: "es-MX".into(),
        voice_id: "ef_dora".into(),
        model_id: "hexgrad/kokoro-82m".into(),
    }
}

#[tokio::test]
async fn openrouter_tts_rest_auth_payload_and_binary_mp3() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .and(path(
            "/openrouter/audio/speech",
        ))
        .and(header("authorization", "Bearer fixture-token"))
        .and(header("content-type", "application/json"))
        .and(header("cf-aig-collect-log", "false"))
        .and(header("cf-aig-skip-cache", "true"))
        .and(header("cf-aig-authorization", "Bearer fixture-gateway"))
        .and(body_json(serde_json::json!({
            "input": request().text, "model": Model::Kokoro.id(), "voice": "ef_dora", "response_format": "mp3",
            "provider": {"order":["deepinfra/us","together"],"only":["deepinfra/us","together"],"sort":"price","allow_fallbacks":true,"data_collection":"deny","zdr":true},
        })))
        .respond_with(
            ResponseTemplate::new(200)
                .set_body_bytes(MP3)
                .insert_header("content-type", "audio/mpeg"),
        )
        .expect(1)
        .mount(&server)
        .await;
    let provider = OpenRouterSpeech::new(
        "https://openrouter.ai/api/v1",
        Some((
            &format!("{}/openrouter/", server.uri()),
            Some("fixture-gateway"),
        )),
        "fixture-token",
        Model::Kokoro,
    )
    .unwrap();
    let out = provider.synthesize(&request()).await.unwrap();
    assert_eq!(out.format, "audio/mpeg");
    assert_eq!(out.audio, MP3);
    assert!(
        (950..=1075).contains(&out.duration_ms),
        "{}",
        out.duration_ms
    );
    assert_eq!(out.alignment.end_ms.last(), Some(&out.duration_ms));
    validate(&request().text, &out).unwrap();
}

#[tokio::test]
async fn openrouter_tts_rejects_json_corruption_and_permanent_vendor_errors() {
    for response in [
        ResponseTemplate::new(200).set_body_json(serde_json::json!({"audio":"abc"})),
        ResponseTemplate::new(200)
            .set_body_bytes(b"invalid audio")
            .insert_header("content-type", "audio/mpeg"),
        ResponseTemplate::new(401),
        ResponseTemplate::new(402),
        ResponseTemplate::new(429),
        ResponseTemplate::new(422),
    ] {
        let server = MockServer::start().await;
        Mock::given(method("POST"))
            .respond_with(response)
            .expect(1)
            .mount(&server)
            .await;
        let provider =
            OpenRouterSpeech::new(&server.uri(), None, "fixture-token", Model::Kokoro).unwrap();
        let err = provider.synthesize(&request()).await.unwrap_err();
        assert!(
            matches!(
                err.downcast_ref::<VendorFailure>(),
                Some(VendorFailure::Permanent(_))
            ),
            "{err}"
        );
    }
}

#[tokio::test]
async fn openrouter_tts_recovers_directly_after_gateway_route_refusal() {
    let gateway = MockServer::start().await;
    let direct = MockServer::start().await;
    Mock::given(method("POST"))
        .respond_with(
            ResponseTemplate::new(400).set_body_json(serde_json::json!({"errors":[{"code":2005}]})),
        )
        .expect(1)
        .mount(&gateway)
        .await;
    Mock::given(method("POST"))
        .and(path("/api/v1/audio/speech"))
        .and(header("authorization", "Bearer fixture-token"))
        .and(|request: &wiremock::Request| {
            !request.headers.contains_key("cf-aig-authorization")
                && serde_json::from_slice::<serde_json::Value>(&request.body).is_ok_and(|body| {
                    body["provider"]["zdr"] == true && body["provider"]["data_collection"] == "deny"
                })
        })
        .respond_with(
            ResponseTemplate::new(200)
                .set_body_bytes(MP3)
                .insert_header("content-type", "audio/mpeg"),
        )
        .expect(1)
        .mount(&direct)
        .await;
    let provider = OpenRouterSpeech::new(
        &format!("{}/api/v1", direct.uri()),
        Some((&gateway.uri(), Some("gateway-only"))),
        "fixture-token",
        Model::Kokoro,
    )
    .unwrap();
    assert_eq!(provider.synthesize(&request()).await.unwrap().audio, MP3);
}
