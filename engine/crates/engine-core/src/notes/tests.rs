use super::*;
use crate::{Completion, LlmProvider, ProviderRouter};
use async_trait::async_trait;
use std::sync::Arc;

struct MockNotesProvider {
    output: String,
    should_fail: bool,
}

#[async_trait]
impl LlmProvider for MockNotesProvider {
    fn name(&self) -> &'static str {
        "mock_notes"
    }
    async fn complete(&self, _model: &str, _input: &str) -> anyhow::Result<Completion> {
        if self.should_fail {
            anyhow::bail!("mock provider error");
        }
        Ok(Completion {
            text: self.output.clone(),
            provider: "mock".into(),
            model: "claude-haiku-5-5".into(),
            input_tokens: 100,
            output_tokens: 50,
            cache_write_tokens: 0,
            cache_read_tokens: 0,
            usage_unavailable: false,
        })
    }
    async fn stream(&self, _model: &str, _input: &str) -> anyhow::Result<crate::CompletionStream> {
        unimplemented!()
    }
}

fn test_limits() -> crate::credits::Limits {
    crate::credits::Limits {
        input_tokens: 1200,
        output_tokens: 300,
        timeout_ms: 5000,
        max_attempts: 2,
        max_cost_nanos: 2_000_000,
        input_nanos: 100,
        output_nanos: 500,
        cache_write_nanos: 125,
        cache_read_nanos: 10,
        deadline_unix_ms: 0,
    }
}

#[tokio::test]
async fn test_enrich_note_grounds_links_and_filters_hallucinations() {
    let raw_output = serde_json::json!({
        "summary": "Apparent star motion comes from Earth rotation; Polaris barely moves.",
        "tags": ["rotation", "polaris"],
        "links": ["lesson-2", "foreign-hallucinated-lesson"]
    })
    .to_string();

    let provider = Arc::new(MockNotesProvider {
        output: raw_output,
        should_fail: false,
    });
    let router = ProviderRouter::new(provider.clone(), provider).unwrap();

    let allowed_lessons = vec![
        LessonRef {
            lesson_id: "lesson-1".into(),
            title: "Why the sky changes".into(),
            goal: "Observe star motion".into(),
        },
        LessonRef {
            lesson_id: "lesson-2".into(),
            title: "Following a star".into(),
            goal: "Track Polaris".into(),
        },
    ];

    let limits = test_limits();

    let outcome = enrich(
        &router,
        "Stars move because Earth spins. Does Polaris move?",
        None,
        None,
        &allowed_lessons,
        &limits,
    )
    .await
    .unwrap();

    let result = outcome.result.expect("expected enrichment result");
    assert_eq!(
        result.summary,
        "Apparent star motion comes from Earth rotation; Polaris barely moves."
    );
    assert_eq!(result.tags, vec!["rotation", "polaris"]);
    // foreign-hallucinated-lesson MUST be filtered out!
    assert_eq!(result.links, vec!["lesson-2"]);
}

#[tokio::test]
async fn test_enrich_note_empty_and_non_english_profile() {
    let raw_output = serde_json::json!({
        "summary": "Trái Đất tự quay quanh trục từ tây sang đông.",
        "tags": ["thiên-văn", "trái-đất"],
        "links": []
    })
    .to_string();

    let provider = Arc::new(MockNotesProvider {
        output: raw_output,
        should_fail: false,
    });
    let router = ProviderRouter::new(provider.clone(), provider).unwrap();

    let limits = test_limits();

    let profile = LearnerProfile {
        locale: "vi-VN".into(),
        level: "beginner".into(),
        goal: "Học thiên văn".into(),
    };

    let outcome = enrich(
        &router,
        "Các ngôi sao dường như di chuyển do Trái Đất tự quay.",
        None,
        Some(&profile),
        &[],
        &limits,
    )
    .await
    .unwrap();

    let result = outcome.result.expect("expected enrichment result");
    assert!(result.summary.contains("Trái Đất"));
    assert_eq!(result.tags, vec!["thiên-văn", "trái-đất"]);
}
