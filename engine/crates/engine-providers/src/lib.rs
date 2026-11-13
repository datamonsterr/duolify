use anyhow::{Context, Result, bail};
use async_trait::async_trait;
use engine_core::{Completion, CompletionStream, LlmProvider, StreamEvent};
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
    fn request(
        &self,
        model: &str,
        input: &str,
        streaming: bool,
        max_output: u32,
    ) -> reqwest::RequestBuilder {
        let mut body = json!({"model":model,"messages":[{"role":"user","content":input}],"stream":streaming,"max_tokens":max_output});
        let mut req = self.client.post(&self.url);
        if self.anthropic {
            body["max_tokens"] = json!(max_output);
            req = req
                .header("x-api-key", &self.key)
                .header("anthropic-version", "2023-06-01");
        } else {
            req = req.bearer_auth(&self.key);
        }
        if streaming && !self.anthropic {
            body["stream_options"] = json!({"include_usage":true});
        }
        req.json(&body)
    }
}
#[async_trait]
impl LlmProvider for HttpProvider {
    fn name(&self) -> &'static str {
        if self.anthropic {
            "anthropic"
        } else {
            "openrouter"
        }
    }
    async fn complete(&self, model: &str, input: &str) -> Result<Completion> {
        self.complete_bounded(model, input, 4096).await
    }
    async fn complete_bounded(
        &self,
        model: &str,
        input: &str,
        max_output: u32,
    ) -> Result<Completion> {
        let value: Value = self
            .request(model, input, false, max_output)
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
            usage_unavailable: [
                if self.anthropic {
                    "input_tokens"
                } else {
                    "prompt_tokens"
                },
                if self.anthropic {
                    "output_tokens"
                } else {
                    "completion_tokens"
                },
            ]
            .into_iter()
            .any(|key| {
                usage[key]
                    .as_u64()
                    .and_then(|v| u32::try_from(v).ok())
                    .is_none()
            }),
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
            })
            .saturating_sub(if self.anthropic {
                0
            } else {
                usage["prompt_tokens_details"]["cached_tokens"]
                    .as_u64()
                    .and_then(|n| u32::try_from(n).ok())
                    .unwrap_or(0)
            }),
            output_tokens: count(if self.anthropic {
                "output_tokens"
            } else {
                "completion_tokens"
            }),
            cache_write_tokens: if self.anthropic {
                count("cache_creation_input_tokens")
            } else {
                0
            },
            cache_read_tokens: if self.anthropic {
                count("cache_read_input_tokens")
            } else {
                usage["prompt_tokens_details"]["cached_tokens"]
                    .as_u64()
                    .and_then(|n| u32::try_from(n).ok())
                    .unwrap_or(0)
            },
        })
    }
    async fn stream(&self, model: &str, input: &str) -> Result<CompletionStream> {
        let response = self
            .request(model, input, true, 4096)
            .send()
            .await?
            .error_for_status()?;
        let anthropic = self.anthropic;
        let model = model.to_owned();
        let output = stream::unfold(
            (
                Box::pin(response.bytes_stream()),
                Vec::<u8>::new(),
                false,
                None::<u32>,
                None::<u32>,
                0_u32,
                0_u32,
                false,
            ),
            move |(
                mut bytes,
                mut buffer,
                mut done,
                mut input,
                mut output,
                mut write,
                mut read,
                mut emitted,
            )| {
                let model = model.clone();
                async move {
                    loop {
                        if let Some(pos) = buffer.iter().position(|b| *b == b'\n') {
                            let line = match String::from_utf8(buffer.drain(..=pos).collect()) {
                                Ok(s) => s,
                                Err(e) => {
                                    return Some((
                                        Err(e.into()),
                                        (bytes, buffer, true, input, output, write, read, emitted),
                                    ));
                                }
                            };
                            if let Some(data) = line.trim().strip_prefix("data:") {
                                let data = data.trim();
                                if data == "[DONE]" {
                                    done = true;
                                    continue;
                                }
                                let v: Value = match serde_json::from_str(data) {
                                    Ok(v) => v,
                                    Err(e) => {
                                        return Some((
                                            Err(e.into()),
                                            (
                                                bytes, buffer, true, input, output, write, read,
                                                emitted,
                                            ),
                                        ));
                                    }
                                };
                                if v["type"] == "error" || !v["error"].is_null() {
                                    return Some((
                                        Err(anyhow::anyhow!("provider stream error")),
                                        (bytes, buffer, true, input, output, write, read, true),
                                    ));
                                }
                                let count =
                                    |v: &Value| v.as_u64().and_then(|n| u32::try_from(n).ok());
                                if anthropic {
                                    if let Some(n) =
                                        count(&v["message"]["usage"]["cache_creation_input_tokens"])
                                    {
                                        write = n
                                    }
                                    if let Some(n) =
                                        count(&v["message"]["usage"]["cache_read_input_tokens"])
                                    {
                                        read = n
                                    }
                                    if let Some(n) = count(&v["message"]["usage"]["input_tokens"]) {
                                        input = Some(n);
                                    }
                                    if let Some(n) = count(&v["usage"]["output_tokens"]) {
                                        output = Some(n);
                                    }
                                    if v["type"] == "message_stop" {
                                        done = true;
                                    }
                                } else {
                                    if let Some(n) =
                                        count(&v["usage"]["prompt_tokens_details"]["cached_tokens"])
                                    {
                                        read = n
                                    }
                                    if let Some(n) = count(&v["usage"]["prompt_tokens"]) {
                                        input = Some(n);
                                    }
                                    if let Some(n) = count(&v["usage"]["completion_tokens"]) {
                                        output = Some(n);
                                    }
                                }
                                let delta = if anthropic {
                                    v["delta"]["text"].as_str()
                                } else {
                                    v["choices"][0]["delta"]["content"].as_str()
                                };
                                if let Some(text) = delta {
                                    return Some((
                                        Ok(StreamEvent::Delta(text.to_owned())),
                                        (bytes, buffer, done, input, output, write, read, emitted),
                                    ));
                                }
                            }
                        } else if done {
                            if emitted {
                                return None;
                            }
                            emitted = true;
                            let usage = Completion {
                                text: String::new(),
                                provider: if anthropic { "anthropic" } else { "openrouter" }.into(),
                                model,
                                input_tokens: if anthropic {
                                    input.unwrap_or(0)
                                } else {
                                    input.unwrap_or(0).saturating_sub(read)
                                },
                                output_tokens: output.unwrap_or(0),
                                cache_write_tokens: write,
                                cache_read_tokens: read,
                                usage_unavailable: input.is_none() || output.is_none(),
                            };
                            return Some((
                                Ok(StreamEvent::Usage(usage)),
                                (bytes, buffer, done, input, output, write, read, emitted),
                            ));
                        } else {
                            match bytes.next().await {
                                Some(Ok(b)) => buffer.extend_from_slice(&b),
                                Some(Err(e)) => {
                                    return Some((
                                        Err(e.into()),
                                        (bytes, buffer, true, input, output, write, read, true),
                                    ));
                                }
                                None => {
                                    return Some((
                                        Err(anyhow::anyhow!(
                                            "provider stream ended before completion"
                                        )),
                                        (bytes, Vec::new(), true, input, output, write, read, true),
                                    ));
                                }
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
    if mode != "live" {
        bail!("unknown provider mode: '{mode}'");
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
