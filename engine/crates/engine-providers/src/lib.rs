use anyhow::{Context, Result, bail};
use async_trait::async_trait;
use engine_core::{Completion, CompletionStream, LlmProvider};
use futures::{StreamExt, stream};
use serde_json::{Value, json};

pub struct HttpProvider {
    client: reqwest::Client,
    url: String,
    key: String,
    anthropic: bool,
}
pub type AnthropicProvider = HttpProvider;
pub type OpenRouterProvider = HttpProvider;
impl HttpProvider {
    pub fn anthropic(key: String, url: String) -> Self {
        Self {
            client: reqwest::Client::new(),
            url,
            key,
            anthropic: true,
        }
    }
    pub fn openrouter(key: String, url: String) -> Self {
        Self {
            client: reqwest::Client::new(),
            url,
            key,
            anthropic: false,
        }
    }
    fn request(&self, model: &str, input: &str, streaming: bool) -> reqwest::RequestBuilder {
        let mut body =
            json!({"model":model,"messages":[{"role":"user","content":input}],"stream":streaming});
        let mut req = self.client.post(&self.url);
        if self.anthropic {
            body["max_tokens"] = json!(4096);
            req = req
                .header("x-api-key", &self.key)
                .header("anthropic-version", "2023-06-01");
        } else {
            req = req.bearer_auth(&self.key);
        }
        req.json(&body)
    }
}
#[async_trait]
impl LlmProvider for HttpProvider {
    async fn complete(&self, model: &str, input: &str) -> Result<Completion> {
        let value: Value = self
            .request(model, input, false)
            .send()
            .await?
            .error_for_status()?
            .json()
            .await?;
        let (text, usage) = if self.anthropic {
            (&value["content"][0]["text"], &value["usage"])
        } else {
            (&value["choices"][0]["message"]["content"], &value["usage"])
        };
        let text = text
            .as_str()
            .context("missing completion text")?
            .to_string();
        let count = |key: &str| {
            usage[key]
                .as_u64()
                .and_then(|v| u32::try_from(v).ok())
                .unwrap_or(0)
        };
        Ok(Completion {
            text,
            provider: if self.anthropic {
                "anthropic"
            } else {
                "openrouter"
            }
            .into(),
            model: model.into(),
            input_tokens: count(if self.anthropic {
                "input_tokens"
            } else {
                "prompt_tokens"
            }),
            output_tokens: count(if self.anthropic {
                "output_tokens"
            } else {
                "completion_tokens"
            }),
        })
    }
    async fn stream(&self, model: &str, input: &str) -> Result<CompletionStream> {
        let response = self
            .request(model, input, true)
            .send()
            .await?
            .error_for_status()?;
        let anthropic = self.anthropic;
        let bytes = response.bytes_stream();
        let output = stream::unfold(
            (Box::pin(bytes), String::new(), false),
            move |(mut bytes, mut buffer, mut done)| async move {
                loop {
                    if let Some(pos) = buffer.find('\n') {
                        let line = buffer.drain(..=pos).collect::<String>();
                        if let Some(data) = line.trim().strip_prefix("data:") {
                            let data = data.trim();
                            if data == "[DONE]" {
                                done = true;
                                continue;
                            }
                            match serde_json::from_str::<Value>(data) {
                                Ok(v) => {
                                    let delta = if anthropic {
                                        v["delta"]["text"].as_str()
                                    } else {
                                        v["choices"][0]["delta"]["content"].as_str()
                                    };
                                    if let Some(text) = delta {
                                        return Some((Ok(text.to_string()), (bytes, buffer, done)));
                                    }
                                }
                                Err(e) => return Some((Err(e.into()), (bytes, buffer, true))),
                            }
                        }
                    } else if done {
                        return None;
                    } else {
                        match bytes.next().await {
                            Some(Ok(b)) => match std::str::from_utf8(&b) {
                                Ok(s) => buffer.push_str(s),
                                Err(e) => return Some((Err(e.into()), (bytes, buffer, true))),
                            },
                            Some(Err(e)) => return Some((Err(e.into()), (bytes, buffer, true))),
                            None => {
                                if !buffer.is_empty() {
                                    buffer.push('\n');
                                }
                                done = true;
                            }
                        }
                    }
                }
            },
        );
        Ok(Box::pin(output))
    }
}
pub fn router(mode: &str) -> Result<engine_core::ProviderRouter> {
    use std::sync::Arc;
    if mode == "mock" {
        return engine_core::ProviderRouter::new(
            Arc::new(engine_core::MockProvider),
            Arc::new(engine_core::MockProvider),
        );
    }
    let anthropic = std::env::var("ANTHROPIC_API_KEY").context("ANTHROPIC_API_KEY missing")?;
    let openrouter = std::env::var("OPENROUTER_API_KEY").context("OPENROUTER_API_KEY missing")?;
    if anthropic.is_empty() || openrouter.is_empty() {
        bail!("provider keys must not be empty");
    }
    engine_core::ProviderRouter::new(
        Arc::new(HttpProvider::anthropic(
            anthropic,
            "https://api.anthropic.com/v1/messages".into(),
        )),
        Arc::new(HttpProvider::openrouter(
            openrouter,
            "https://openrouter.ai/api/v1/chat/completions".into(),
        )),
    )
}
