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
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Completion {
    pub text: String,
    pub provider: String,
    pub model: String,
    pub input_tokens: u32,
    pub output_tokens: u32,
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
    async fn complete(&self, model: &str, _: &str) -> Result<Completion> {
        Ok(Completion { text: serde_json::json!({"title":"Sample", "summary":"Sample note", "html":"<div id=\"root\">Sample</div>", "units":[{"unit_id":"unit-1","title":"Sample unit","lessons":[{"lesson_id":"lesson-1","title":"Sample lesson","goal":"Learn"}]}], "test":[{"question_id":"q1","prompt":"Sample?","choices":["Yes","No"],"answer_index":0,"explanation":"Sample"}]}).to_string(), provider: "mock".into(), model: model.into(), input_tokens: 0, output_tokens: 0 })
    }
    async fn stream(&self, model: &str, _: &str) -> Result<CompletionStream> {
        let usage = Completion {
            text: String::new(),
            provider: "mock".into(),
            model: model.into(),
            input_tokens: 0,
            output_tokens: 0,
        };
        Ok(Box::pin(futures::stream::iter(vec![
            Ok(StreamEvent::Delta("Sample response".into())),
            Ok(StreamEvent::Usage(usage)),
        ])))
    }
}
pub mod slide;
#[cfg(test)]
mod tests;
