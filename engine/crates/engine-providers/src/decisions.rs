//! OpenRouter Decisions API client for the skill router (ADR 0025). One `noul` question per
//! candidate skill; the answer is P(yes). The endpoint is alpha, so this sits behind
//! `SkillRouter` and every failure is the caller's cue to use the default set.
use anyhow::{Context, Result, bail};
use async_trait::async_trait;
use engine_core::router::{Candidate, RouterInput, SkillRouter};
use serde_json::{Value, json};
use std::collections::BTreeMap;

/// Router model (ADR 0025 D4, chosen on the spike's router bench; re-decided on the router golden set).
pub const ROUTER_MODEL: &str = "microsoft/microsoft-decision-1.1";
pub const ENDPOINT: &str = "https://openrouter.ai/api/alpha/decisions";

pub struct DecisionsRouter {
    client: reqwest::Client,
    url: String,
    key: String,
    model: String,
}

impl DecisionsRouter {
    /// `url` is the full endpoint (`https://openrouter.ai/api/alpha/decisions`).
    pub fn new(key: String, url: String, model: String) -> Self {
        Self {
            client: reqwest::Client::new(),
            url,
            key,
            model,
        }
    }

    fn body(&self, input: &RouterInput, candidates: &[Candidate]) -> Value {
        let questions: serde_json::Map<String, Value> = candidates
            .iter()
            .map(|c| {
                (
                    c.id.clone(),
                    json!({"type":"noul","instructions":format!(
                        "Would this {} request benefit from the guidance: {}", input.task, c.description)}),
                )
            })
            .collect();
        json!({
            "model": self.model,
            "state": serde_json::to_string(input).unwrap_or_default(),
            "questions": questions,
            // Learner-derived titles must never be used for provider training.
            "provider": {"data_collection": "deny"},
        })
    }
}

/// `answers.<id> = {type: "noul", noul: p}`; anything else is ignored, a missing `answers` is
/// an error.
pub fn parse(value: &Value) -> Result<BTreeMap<String, f64>> {
    let answers = value
        .get("answers")
        .and_then(Value::as_object)
        .context("decisions answer without answers")?;
    Ok(answers
        .iter()
        .filter(|(_, a)| a["type"] == "noul")
        .filter_map(|(k, a)| Some((k.clone(), a["noul"].as_f64()?)))
        .collect())
}

#[async_trait]
impl SkillRouter for DecisionsRouter {
    async fn decide(
        &self,
        input: &RouterInput,
        candidates: &[Candidate],
    ) -> Result<BTreeMap<String, f64>> {
        let response = self
            .client
            .post(&self.url)
            .bearer_auth(&self.key)
            .json(&self.body(input, candidates))
            .send()
            .await?;
        if !response.status().is_success() {
            bail!("decisions status {}", response.status());
        }
        parse(&response.json().await?)
    }
}
