use super::*;

#[test]
fn config_has_all_tasks() {
    const HY4: &str = "tencent/hy4-flash";
    const GLM: &str = "z-ai/glm-5.3-flash";
    let config: Models = toml::from_str(include_str!("../../../config/models.toml")).unwrap();
    assert_eq!(config.tasks.len(), 15);
    for (task, effort) in [
        ("lesson", "none"),
        ("published_copy_lesson", "none"),
        ("note", "none"),
        ("setup_chat", "none"),
        ("course_brief_chat", "none"),
        ("outline_chat_change", "none"),
        ("chat", "none"),
        ("preview", "low"),
        ("outline", "low"),
        ("outline_replan", "low"),
        ("character_create", "low"),
        ("character_screen", "low"),
        ("course_screen", "low"),
        ("steer_screen", "low"),
        ("screen_review", "low"),
    ] {
        let route = &config.tasks[task];
        assert_eq!(route.model, HY4, "{task}");
        assert_eq!(route.fallback_models, [GLM], "{task}");
        assert_eq!(route.fallback_model, GLM, "{task}");
        assert_eq!(route.reasoning_effort.as_deref(), Some(effort), "{task}");
        // Effort is per model: GLM cannot disable reasoning, so it is always low.
        assert_eq!(route.reasoning[HY4], effort, "{task}");
        assert_eq!(route.reasoning[GLM], "low", "{task}");
    }
}

#[test]
fn routes_carry_chain_and_per_model_reasoning() {
    let config: Models = toml::from_str(include_str!("../../../config/models.toml")).unwrap();
    let lesson = config.tasks["lesson"].primary_route();
    assert_eq!(lesson.model, "tencent/hy4-flash");
    assert_eq!(lesson.fallbacks, ["z-ai/glm-5.3-flash"]);
    assert_eq!(lesson.reasoning["tencent/hy4-flash"], "none");
    let fallback = config.tasks["lesson"].fallback_route();
    assert_eq!(fallback.model, "z-ai/glm-5.3-flash");
    assert!(fallback.fallbacks.is_empty());
    assert_eq!(fallback.reasoning["z-ai/glm-5.3-flash"], "low");
    let legacy: Model = toml::from_str("model = \"a\"\nfallback_model = \"b\"").unwrap();
    assert_eq!(legacy.fallback_models, ["b"]);
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
