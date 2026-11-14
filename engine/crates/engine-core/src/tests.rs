use super::*;

#[test]
fn config_has_all_tasks() {
    let config: Models = toml::from_str(include_str!("../../../config/models.toml")).unwrap();
    assert_eq!(config.tasks.len(), 13);
    assert_eq!(
        config.tasks["published_copy_lesson"].model,
        "claude-sonnet-5-5"
    );
    assert_eq!(config.tasks["character_create"].model, "claude-haiku-5-5");
    assert_eq!(
        config.tasks["character_create"].fallback_model,
        "anthropic/claude-haiku-5.5"
    );
    assert_eq!(config.tasks["character_screen"].model, "claude-haiku-5-5");
    assert_eq!(
        config.tasks["character_screen"].fallback_model,
        "anthropic/claude-haiku-5.5"
    );
    assert_eq!(config.tasks["chat"].model, "claude-haiku-5-5");
    assert_eq!(config.tasks["setup_chat"].model, "claude-haiku-5-5");
    assert_eq!(
        config.tasks["outline_chat_change"].model,
        "claude-haiku-5-5"
    );
    assert_eq!(
        config.tasks["outline_chat_change"].fallback_model,
        "anthropic/claude-haiku-5.5"
    );
}

#[test]
fn slide_rejects_invalid_documents() {
    assert!(slide::validate("<div id=\"root\">hello</div>", &[]).is_ok());
    assert!(slide::validate("<div>hello</div>", &[]).is_err());
    assert!(
        slide::validate(
            "<div id=\"root\"><img src=\"https://evil.test/x\"></div>",
            &[]
        )
        .is_err()
    );
    assert!(
        slide::validate(
            "<div id=\"root\"><img src=\"https://safe.test/x\"></div>",
            &["safe.test"]
        )
        .is_ok()
    );
    assert!(
        slide::validate(
            &format!("<div id=\"root\">{}</div>", "x".repeat(slide::MAX_BYTES)),
            &[]
        )
        .is_err()
    );
}

#[test]
fn slide_rejects_network_obfuscation_and_fake_roots() {
    assert!(slide::validate("<!-- <div id=\"root\"></div> -->", &[]).is_err());
    assert!(slide::validate("<div id=\"root\"></div><script>fetch('/api')</script>", &[]).is_err());
    assert!(
        slide::validate(
            "<div id=\"root\"></div><img src=\"https://safe.test@evil.test/x\">",
            &["safe.test"]
        )
        .is_err()
    );
    assert!(
        slide::validate(
            "<div id=\"root\"></div><script>fetch('https:&#47;&#47;evil')</script>",
            &[]
        )
        .is_err()
    );
}

#[tokio::test]
async fn router_retries_then_falls_back() {
    use std::sync::{
        Arc,
        atomic::{AtomicUsize, Ordering},
    };
    struct Failing(AtomicUsize);
    #[async_trait::async_trait]
    impl LlmProvider for Failing {
        fn name(&self) -> &'static str {
            "failing"
        }
        async fn complete(&self, _: &str, _: &str) -> anyhow::Result<Completion> {
            self.0.fetch_add(1, Ordering::SeqCst);
            anyhow::bail!("failed")
        }
        async fn stream(&self, _: &str, _: &str) -> anyhow::Result<CompletionStream> {
            anyhow::bail!("failed")
        }
    }
    let primary = Arc::new(Failing(AtomicUsize::new(0)));
    let router = ProviderRouter::new(primary.clone(), Arc::new(MockProvider)).unwrap();
    assert_eq!(
        router.complete("note", "test").await.unwrap().provider,
        "mock"
    );
    // Must make at most 1 attempt on primary before fallback (never 2 retries = 3 calls)
    assert_eq!(primary.0.load(Ordering::SeqCst), 1);
}
