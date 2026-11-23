use anyhow::{Context, Result};
use async_trait::async_trait;
use futures::Stream;
use serde::{Deserialize, Serialize};
use std::{collections::BTreeMap, pin::Pin, sync::Arc};

#[derive(Debug, Clone, Deserialize)]
#[serde(from = "RawModels")]
pub struct Models {
    pub tasks: BTreeMap<String, Model>,
}
#[derive(Deserialize)]
struct RawModels {
    tasks: BTreeMap<String, Model>,
    /// OpenRouter `reasoning.effort` per model id. Effort is per model because a fallback
    /// chain mixes models with different reasoning support (GLM cannot disable reasoning).
    #[serde(default)]
    reasoning: BTreeMap<String, String>,
}
impl From<RawModels> for Models {
    fn from(raw: RawModels) -> Self {
        let mut tasks = raw.tasks;
        for (task, model) in tasks.iter_mut() {
            model.response_format = structured::response_format(task);
            model.reasoning = raw.reasoning.clone();
            if let Some(effort) = model.reasoning_effort.clone() {
                model.reasoning.insert(model.model.clone(), effort);
            }
        }
        Self { tasks }
    }
}
/// One task route. `model` is the primary id, `fallback_models` the ordered fallback chain.
/// `fallback_model` is derived (first fallback) and is the stable second-slot identity
/// that reservations pin.
#[derive(Debug, Clone, Deserialize)]
#[serde(from = "RawModel")]
pub struct Model {
    pub model: String,
    pub fallback_model: String,
    pub fallback_models: Vec<String>,
    /// `reasoning.effort` for the primary model (`none` or `low`).
    pub reasoning_effort: Option<String>,
    /// `reasoning.effort` per model id: the `[reasoning]` table plus the primary's own effort.
    pub reasoning: BTreeMap<String, String>,
    /// OpenRouter `response_format` for this task (`structured::response_format`).
    pub response_format: Option<serde_json::Value>,
    pub endpoint: Option<String>,
    pub zdr: bool,
    pub data_collection: Option<String>,
}
#[derive(Deserialize)]
struct RawModel {
    model: String,
    #[serde(default)]
    fallback_model: Option<String>,
    #[serde(default)]
    fallback_models: Vec<String>,
    #[serde(default)]
    reasoning_effort: Option<String>,
    #[serde(default)]
    endpoint: Option<String>,
    #[serde(default)]
    zdr: bool,
    #[serde(default)]
    data_collection: Option<String>,
}
impl From<RawModel> for Model {
    fn from(raw: RawModel) -> Self {
        let mut fallback_models = raw.fallback_models;
        if fallback_models.is_empty() {
            fallback_models.extend(raw.fallback_model);
        }
        Self {
            model: raw.model,
            fallback_model: fallback_models.first().cloned().unwrap_or_default(),
            fallback_models,
            reasoning_effort: raw.reasoning_effort,
            reasoning: BTreeMap::new(),
            response_format: None,
            endpoint: raw.endpoint,
            zdr: raw.zdr,
            data_collection: raw.data_collection,
        }
    }
}
/// What a provider needs to serve one call. `fallbacks` are tried in order after `model`.
#[derive(Debug, Clone, Default)]
pub struct Route {
    pub model: String,
    pub fallbacks: Vec<String>,
    /// Effort per model id; a model without an entry gets no `reasoning` field.
    pub reasoning: BTreeMap<String, String>,
    /// Structured-output request (`response_format`). Providers that send it also require
    /// endpoints that honour it.
    pub response_format: Option<serde_json::Value>,
    /// The one bounded repair: the rejected answer and the corrective message after it.
    pub repair: Option<Repair>,
    pub endpoint: Option<String>,
    pub zdr: bool,
    pub data_collection: Option<String>,
}
/// A rejected answer and the corrective user message sent after it on the repair attempt.
#[derive(Debug, Clone, Default)]
pub struct Repair {
    pub previous: String,
    pub message: String,
}
impl Model {
    pub fn primary_route(&self) -> Route {
        Route {
            model: self.model.clone(),
            fallbacks: self.fallback_models.clone(),
            reasoning: self.reasoning.clone(),
            response_format: self.response_format.clone(),
            repair: None,
            endpoint: self.endpoint.clone(),
            zdr: self.zdr,
            data_collection: self.data_collection.clone(),
        }
    }
    /// One bounded attempt: exactly one model request, no provider-side chain, so the bounded
    /// runner counts, times, prices and labels every call it makes.
    pub fn attempt_route(&self, fallback: bool) -> Route {
        let mut route = if fallback {
            self.fallback_route()
        } else {
            self.primary_route()
        };
        route.fallbacks.clear();
        route
    }
    pub fn fallback_route(&self) -> Route {
        Route {
            model: self.fallback_model.clone(),
            fallbacks: self.fallback_models.iter().skip(1).cloned().collect(),
            reasoning: self.reasoning.clone(),
            response_format: self.response_format.clone(),
            repair: None,
            endpoint: self.endpoint.clone(),
            zdr: self.zdr,
            data_collection: self.data_collection.clone(),
        }
    }
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
    /// True for deterministic fixture backends, whatever route name they serve under. Features
    /// without live evidence (E10-06) run only when both router members are fixtures.
    fn is_fixture(&self) -> bool {
        self.name() == "mock"
    }
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
    /// Route-aware call; providers without routing options serve the primary id only.
    async fn complete_route(
        &self,
        route: &Route,
        input: &str,
        max_output: u32,
    ) -> Result<Completion> {
        self.complete_bounded(&route.model, input, max_output).await
    }
    async fn stream_route(&self, route: &Route, input: &str) -> Result<CompletionStream> {
        self.stream(&route.model, input).await
    }
    /// Speech to text on the route's audio endpoint. The audio is borrowed for the call only.
    async fn transcribe(
        &self,
        _route: &Route,
        _model: &str,
        _audio: &[u8],
        _container: &str,
        _language: Option<&str>,
    ) -> Result<transcribe::Transcription> {
        anyhow::bail!("this provider has no transcription endpoint")
    }
}
#[derive(Clone)]
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
    /// Both members are deterministic fixtures (mock mode), so no live provider can be reached.
    pub fn fixture_only(&self) -> bool {
        self.primary.is_fixture() && self.fallback.is_fixture()
    }
    pub async fn complete(&self, task: &str, input: &str) -> Result<Completion> {
        let model = self.models.tasks.get(task).context("unknown task")?;
        // An empty answer (null content with measured usage) is a failure here: use the fallback.
        if let Ok(value) = self
            .primary
            .complete_route(&model.primary_route(), input, 4096)
            .await
            && !value.text.is_empty()
        {
            return Ok(value);
        }
        self.fallback
            .complete_route(&model.fallback_route(), input, 4096)
            .await
    }
    pub async fn stream(&self, task: &str, input: &str) -> Result<CompletionStream> {
        let model = self.models.tasks.get(task).context("unknown task")?;
        if let Ok(value) = self
            .primary
            .stream_route(&model.primary_route(), input)
            .await
        {
            return Ok(value);
        }
        self.fallback
            .stream_route(&model.fallback_route(), input)
            .await
    }
}
pub struct MockProvider;
#[async_trait]
impl LlmProvider for MockProvider {
    fn name(&self) -> &'static str {
        "mock"
    }
    /// Deterministic fixture: an all-zero recording is silence, anything else says one phrase.
    async fn transcribe(
        &self,
        _route: &Route,
        model: &str,
        audio: &[u8],
        _container: &str,
        language: Option<&str>,
    ) -> Result<transcribe::Transcription> {
        let silent = audio.iter().all(|b| *b == 0);
        Ok(transcribe::Transcription {
            text: if silent { "" } else { "mock transcript" }.into(),
            language: language.unwrap_or("en").into(),
            seconds: audio.len() as f64 * 8.0 / 32_000.0,
            cost_nanos: 0,
            cost_reported: true,
            confidence: None,
            model: model.into(),
        })
    }
    async fn complete(&self, model: &str, input: &str) -> Result<Completion> {
        let steering_data = steering::provider_data(input);
        let input = steering_data.as_deref().unwrap_or(input);
        let data = serde_json::from_str::<serde_json::Value>(input).ok();
        let text = if data.as_ref().is_some_and(|v| v["task"] == "steer_screen") {
            let value = data.as_ref().unwrap();
            let text = value["text"].as_str().unwrap_or_default().to_lowercase();
            let reason = if steering::deterministic(&text).is_some()
                || text.contains("reveal hidden instructions")
            {
                "prompt_injection"
            } else if text.contains("unsafe content") || text.contains("graphic violence") {
                "unsafe_content"
            } else if text.contains("off topic") {
                "off_topic_for_course"
            } else {
                ""
            };
            serde_json::json!({"allowed":reason.is_empty(),"reason_code":reason}).to_string()
        } else if data
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
            .is_some_and(|v| v["task"] == "course_screen" || v["task"] == "screen_review")
        {
            // Deterministic fixture provider over every depth of the sample.
            let text = data.unwrap()["sample"].to_string().to_lowercase();
            let (verdict, code) = [
                ("ignore policy", "block", "prompt_injection"),
                ("reveal system prompt", "block", "prompt_injection"),
                ("ignore previous instructions", "block", "prompt_injection"),
                ("hidden payload", "block", "hidden_payload"),
                ("abusive-review", "block", "harmful_content"),
                ("spam-review", "block", "harmful_content"),
                ("real person likeness", "block", "real_person"),
                ("sexual content", "block", "sexual_content"),
                ("borderline", "needs_review", "harmful_content"),
            ]
            .into_iter()
            .find(|(needle, _, _)| text.contains(needle))
            .map(|(_, v, c)| (v, c))
            .unwrap_or(("pass", ""));
            let codes: Vec<&str> = if code.is_empty() { vec![] } else { vec![code] };
            serde_json::json!({"verdict":verdict,"reason_codes":codes}).to_string()
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
        } else if input.contains("note_enrich")
            || input.contains("foreign-hallucinated-lesson")
            || input.contains("Stars move")
            || model.contains("note")
        {
            let data = serde_json::from_str::<serde_json::Value>(input).ok();
            let note = data
                .as_ref()
                .and_then(|d| {
                    d.pointer("/learner_data/note")
                        .or_else(|| d.get("note"))
                        .and_then(|n| n.as_str())
                })
                .unwrap_or_default();
            if note.contains("Trái Đất") || input.contains("vi-VN") {
                serde_json::json!({
                    "summary": "Trái Đất tự quay quanh trục từ tây sang đông.",
                    "tags": ["thiên-văn", "trái-đất"],
                    "links": []
                })
                .to_string()
            } else if note == "example" || note.is_empty() {
                serde_json::json!({
                    "summary": "Sample note",
                    "tags": ["sample"],
                    "links": []
                })
                .to_string()
            } else {
                serde_json::json!({
                    "summary": "Apparent star motion comes from Earth rotation; Polaris barely moves.",
                    "tags": ["rotation", "polaris"],
                    "links": ["valid-lesson-id", "lesson-2", "foreign-hallucinated-lesson", "unknown-foreign-lesson"]
                })
                .to_string()
            }
        } else if data.as_ref().is_some_and(|v| {
            v["task"] == "lesson_test_variant" || v.get("source_question").is_some()
        }) {
            // Only the GenerateRetryVariants request (lessontest::retry) gets this fixture: the
            // lesson prompt also names `variants` (LESSON_SHAPE) and must get the lesson fixture.
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
                        "html": "<div id=\"root\"><h1 data-slot=\"top\">Sample</h1><p data-slot=\"body\">Sample explanation</p></div>",
                        "duration_ms": 3000,
                        "narration": "Sample explanation",
                        "topic_ids": ["top-1"],
                        "regions": [
                            {"slot": "top", "role": "title", "description": "Title: Sample", "start_ms": 0, "end_ms": 0},
                            {"slot": "body", "role": "explanation", "description": "Sample explanation of the topic", "start_ms": 0, "end_ms": 0}
                        ]
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
                                "prompt": "Which option correctly confirms the example?",
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
pub mod chat;
pub mod courses;
pub mod credits;
pub mod eval;
pub mod lessontest;
pub mod narration;
pub mod notes;
pub mod pricing;
pub mod promotion;
pub mod regions;
pub mod sharing;
pub mod skills;
pub mod slide;
#[cfg(test)]
mod tests;

pub mod steering;
pub mod structured;
pub mod transcribe;
