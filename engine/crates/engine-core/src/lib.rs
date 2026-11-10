use anyhow::{Context, Result};
use async_trait::async_trait;
use futures::Stream;
use serde::{Deserialize, Serialize};
use std::{collections::BTreeMap, pin::Pin, sync::Arc};

#[derive(Debug, Deserialize)]
pub struct Models {
    pub tasks: BTreeMap<String, Model>,
}
#[derive(Debug, Deserialize)]
pub struct Model {
    pub model: String,
    pub fallback_model: String,
}
#[derive(Debug, Default, Clone, Serialize, Deserialize)]
pub struct Completion {
    pub text: String,
    pub provider: String,
    pub model: String,
    pub input_tokens: u32,
    pub output_tokens: u32,
    #[serde(default)]
    pub cache_write_tokens: u32,
    #[serde(default)]
    pub cache_read_tokens: u32,
    /// False includes an explicitly measured zero; true retains the cost hold.
    #[serde(default)]
    pub usage_unavailable: bool,
}
#[derive(Debug, Clone)]
pub enum StreamEvent {
    Delta(String),
    Usage(Completion),
}
pub type CompletionStream = Pin<Box<dyn Stream<Item = Result<StreamEvent>> + Send>>;
#[async_trait]
pub trait LlmProvider: Send + Sync {
    fn name(&self) -> &'static str;
    async fn complete(&self, model: &str, input: &str) -> Result<Completion>;
    async fn complete_bounded(
        &self,
        model: &str,
        input: &str,
        _max_output: u32,
    ) -> Result<Completion> {
        self.complete(model, input).await
    }
    async fn stream(&self, model: &str, input: &str) -> Result<CompletionStream>;
}
pub struct ProviderRouter {
    pub models: Models,
    pub primary: Arc<dyn LlmProvider>,
    pub fallback: Arc<dyn LlmProvider>,
}
impl ProviderRouter {
    pub fn new(primary: Arc<dyn LlmProvider>, fallback: Arc<dyn LlmProvider>) -> Result<Self> {
        let models = toml::from_str(include_str!("../../../config/models.toml"))?;
        Ok(Self {
            models,
            primary,
            fallback,
        })
    }
    pub async fn complete(&self, task: &str, input: &str) -> Result<Completion> {
        let model = self.models.tasks.get(task).context("unknown task")?;
        for _ in 0..2 {
            if let Ok(value) = self.primary.complete(&model.model, input).await {
                return Ok(value);
            }
        }
        self.fallback.complete(&model.fallback_model, input).await
    }
    pub async fn stream(&self, task: &str, input: &str) -> Result<CompletionStream> {
        let model = self.models.tasks.get(task).context("unknown task")?;
        for _ in 0..2 {
            if let Ok(value) = self.primary.stream(&model.model, input).await {
                return Ok(value);
            }
        }
        self.fallback.stream(&model.fallback_model, input).await
    }
}
pub struct MockProvider;
#[async_trait]
impl LlmProvider for MockProvider {
    fn name(&self) -> &'static str {
        "mock"
    }
    async fn complete(&self, model: &str, input: &str) -> Result<Completion> {
        let data = serde_json::from_str::<serde_json::Value>(input).ok();
        let text = if data
            .as_ref()
            .is_some_and(|v| v["task"] == "character_screen")
        {
            let value = data.unwrap();
            let text = value["spec"].to_string().to_lowercase();
            // Deterministic fixture provider, never a substitute for live screening.
            let reason = [
                ("reveal system prompt", "prompt_injection"),
                ("ignore policy", "prompt_injection"),
                ("real person likeness", "real_person"),
                ("trademarked mascot", "third_party_ip"),
                ("sexual content", "sexual_content"),
                ("graphic violence", "age_inappropriate"),
            ]
            .into_iter()
            .find(|(needle, _)| text.contains(needle))
            .map(|(_, r)| r)
            .unwrap_or("");
            serde_json::json!({"approved":reason.is_empty(),"reason_code":reason}).to_string()
        } else if data
            .as_ref()
            .is_some_and(|v| v["task"] == "character_create")
        {
            include_str!(
                "../../../../services/api/internal/characters/schema/original.fixture.json"
            )
            .to_string()
        } else if data.as_ref().is_some_and(|v| v["task"] == "course_outline") {
            serde_json::json!({"units":data.unwrap()["outline"]}).to_string()
        } else if serde_json::from_str::<serde_json::Value>(input)
            .ok()
            .is_some_and(|v| v["task"] == "outline_chat_change")
        {
            let data: serde_json::Value = serde_json::from_str(input)?;
            let operation = if let Some(id) = data["outline"][0]["id"].as_str() {
                serde_json::json!({"kind":"retitled","id":id,"title":"Suggested outline"})
            } else {
                serde_json::json!({"kind":"added","id":"mock-unit","title":"Suggested outline","position":0})
            };
            serde_json::json!({"operations":[operation]}).to_string()
        } else if input.contains("course_brief_chat") {
            let data: serde_json::Value = serde_json::from_str(input)?;
            let answer = data["answer"].as_str().unwrap_or_default().to_lowercase();
            serde_json::json!({"is_vague":matches!(answer.as_str(), "something" | "not sure" | "something about space")}).to_string()
        } else if input.contains("setup_chat")
            || input.contains("is_vague")
            || input.contains("vague")
            || input.contains("scenario")
            || model.contains("setup_chat")
        {
            if input.contains("vague_goal") || input.contains("something about space") {
                serde_json::json!({
                    "is_vague": true,
                    "follow_up_message": "Space is a great pick! Is this for fun, for school, or for work?",
                    "extracted_topic": "Astronomy",
                    "extracted_goal": "Something about space",
                    "quick_replies": ["For fun", "For school", "For work", "Skip"],
                    "next_step": "goal",
                    "is_follow_up": true
                })
                .to_string()
            } else if input.contains("followup_bound") || input.contains("is_follow_up: true") {
                serde_json::json!({
                    "is_vague": false,
                    "message": "What level would you describe yourself as?",
                    "is_follow_up": false,
                    "next_step": "level",
                    "quick_replies": ["Beginner", "Intermediate", "Advanced", "Expert", "Skip"]
                })
                .to_string()
            } else if input.contains("skipped") {
                serde_json::json!({
                    "skipped": true,
                    "message": "What level would you describe yourself as?",
                    "is_follow_up": false,
                    "next_step": "level",
                    "extracted_topic": "",
                    "extracted_goal": ""
                })
                .to_string()
            } else if input.contains("non_english")
                || input.contains("vi-VN")
                || input.contains("thiên văn")
            {
                serde_json::json!({
                    "message": "Thiên văn học là một chủ đề tuyệt vời!",
                    "extracted_topic": "Astronomy",
                    "locale": "vi-VN"
                })
                .to_string()
            } else if input.contains("injection") || input.contains("Ignore previous") {
                serde_json::json!({
                    "message": "I understand your goal.",
                    "safe": true,
                    "injection_blocked": true
                })
                .to_string()
            } else if input.contains("summary_fidelity") {
                serde_json::json!({
                    "extracted_topic": "Astronomy",
                    "extracted_goal": "Explain the night sky to my kids",
                    "extracted_level": "beginner",
                    "fidelity": true
                })
                .to_string()
            } else if input.contains("no_save_before_confirm") {
                serde_json::json!({
                    "draft_only": true,
                    "persisted": false,
                    "confirm_required": true
                })
                .to_string()
            } else if input.contains("unavailable_assistant") {
                serde_json::json!({
                    "error": "assistant unavailable",
                    "form_fallback": true
                })
                .to_string()
            } else {
                serde_json::json!({
                    "is_vague": false,
                    "message": "What level would you describe yourself as?",
                    "extracted_topic": "Astronomy",
                    "extracted_goal": "Fun — for my kids",
                    "next_step": "level",
                    "is_follow_up": false,
                    "quick_replies": ["Beginner", "Intermediate", "Advanced", "Expert", "Skip"]
                })
                .to_string()
            }
        } else if input.contains("lesson_test_variant")
            || input.contains("variants")
            || input.contains("source_question")
        {
            serde_json::json!({
                "variants": [
                    {
                        "question_id": "v1-q1",
                        "topic_id": "top-stars",
                        "variant_of": "q1",
                        "prompt": "Which celestial object produces its own light through fusion?",
                        "choices": ["Planet", "Self-luminous star", "Comet"],
                        "answer_index": 1,
                        "explanation": "Stars generate their own light through nuclear fusion."
                    }
                ]
            })
            .to_string()
        } else {
            serde_json::json!({
                "title": "Sample",
                "narration": "Sample explanation",
                "summary": "Sample note",
                "html": "<div id=\"root\">Sample</div>",
                "topic_ids": ["top-1"],
                "slides": [
                    {
                        "slide_id": "s1",
                        "html": "<div id=\"root\">Sample</div>",
                        "duration_ms": 3000,
                        "narration": "Sample explanation",
                        "topic_ids": ["top-1"]
                    }
                ],
                "units": [
                    {
                        "unit_id": "unit-1",
                        "title": "Sample unit",
                        "lessons": [{"lesson_id": "lesson-1", "title": "Sample lesson", "goal": "Learn"}]
                    }
                ],
                "test": [
                    {
                        "question_id": "q1",
                        "topic_id": "top-1",
                        "prompt": "Sample?",
                        "choices": ["Yes", "No"],
                        "answer_index": 0,
                        "explanation": "Sample"
                    }
                ],
                "variant_pool": [
                    {
                        "topic_id": "top-1",
                        "variants": [
                            {
                                "question_id": "v1-q1",
                                "topic_id": "top-1",
                                "variant_of": "q1",
                                "prompt": "Sample? (rephrased)",
                                "choices": ["No", "Yes"],
                                "answer_index": 1,
                                "explanation": "Sample variant"
                            }
                        ]
                    }
                ]
            })
            .to_string()
        };
        Ok(Completion {
            text,
            provider: "mock".into(),
            model: model.into(),
            input_tokens: 0,
            output_tokens: 0,
            cache_write_tokens: 0,
            cache_read_tokens: 0,
            usage_unavailable: false,
        })
    }
    async fn stream(&self, model: &str, _: &str) -> Result<CompletionStream> {
        let usage = Completion {
            text: String::new(),
            provider: "mock".into(),
            model: model.into(),
            input_tokens: 0,
            output_tokens: 0,
            cache_write_tokens: 0,
            cache_read_tokens: 0,
            usage_unavailable: false,
        };
        Ok(Box::pin(futures::stream::iter(vec![
            Ok(StreamEvent::Delta("Sample response".into())),
            Ok(StreamEvent::Usage(usage)),
        ])))
    }
}
pub mod accounts;
pub mod backup;
pub mod characters;
pub mod courses;
pub mod credits;
pub mod eval;
pub mod lessontest;
pub mod pricing;
pub mod slide;
#[cfg(test)]
mod tests;
