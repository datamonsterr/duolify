pub mod elevenlabs;
use anyhow::{Context, Result, bail};
use async_trait::async_trait;
use engine_core::{Completion, CompletionStream, LlmProvider, Route, StreamEvent};
use futures::{StreamExt, stream};
use serde_json::{Value, json};

fn truncation_reason(value: &Value, anthropic: bool) -> Option<&'static str> {
    if anthropic && value["stop_reason"] == "max_tokens" {
        Some("stop_reason=max_tokens")
    } else if !anthropic && value["choices"][0]["finish_reason"] == "length" {
        Some("finish_reason=length")
    } else {
        None
    }
}

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
    /// Single-model routes for a fallback chain; `None` when the route is already one model.
    fn chain(&self, route: &Route) -> Option<Vec<Route>> {
        if self.anthropic || route.fallbacks.is_empty() {
            return None;
        }
        Some(
            std::iter::once(&route.model)
                .chain(&route.fallbacks)
                .map(|model| Route {
                    model: model.clone(),
                    fallbacks: Vec::new(),
                    ..route.clone()
                })
                .collect(),
        )
    }
    fn request(
        &self,
        route: &Route,
        input: &str,
        streaming: bool,
        max_output: u32,
    ) -> reqwest::RequestBuilder {
        self.request_model(route, &route.model, input, streaming, max_output)
    }
    /// One request for one model id; the reasoning effort is looked up per model.
    fn request_model(
        &self,
        route: &Route,
        model: &str,
        input: &str,
        streaming: bool,
        max_output: u32,
    ) -> reqwest::RequestBuilder {
        let mut body = json!({"model":model,"messages":[{"role":"user","content":input}],"stream":streaming,"max_tokens":max_output});
        if !self.anthropic {
            if let Some(effort) = route.reasoning.get(model) {
                body["reasoning"] = json!({"effort":effort});
            }
            // Learner content must never be used for provider training.
            body["provider"] = json!({"data_collection":"deny"});
            if route.zdr {
                body["provider"]["zdr"] = json!(true);
            }
        }
        if let Some(data) = engine_core::steering::provider_data(input) {
            if self.anthropic {
                body["system"] = json!(engine_core::steering::POLICY);
                body["messages"] = json!([{"role":"user","content":data}]);
            } else {
                body["messages"] = json!([{"role":"system","content":engine_core::steering::POLICY},{"role":"user","content":data}]);
            }
        }
        // The one bounded repair replays the rejected answer and a corrective user turn.
        if let Some(repair) = &route.repair
            && let Some(messages) = body["messages"].as_array_mut()
        {
            messages.push(json!({"role":"assistant","content":repair.previous}));
            messages.push(json!({"role":"user","content":repair.message}));
        }
        if !self.anthropic
            && let Some(format) = &route.response_format
        {
            body["response_format"] = format.clone();
            // Only endpoints that honour `response_format` may serve a structured task.
            body["provider"]["require_parameters"] = json!(true);
        }
        let mut req = self.client.post(&self.url);
        if self.anthropic {
            body["max_tokens"] = json!(max_output);
            req = req
                .header("x-api-key", &self.key)
                .header("anthropic-version", "2023-06-01");
        } else {
            req = req
                .bearer_auth(&self.key)
                .header("HTTP-Referer", "https://duolify.online")
                .header("X-Title", "Duolify");
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
        let route = Route {
            model: model.into(),
            ..Route::default()
        };
        self.complete_route(&route, input, max_output).await
    }
    async fn stream(&self, model: &str, input: &str) -> Result<CompletionStream> {
        let route = Route {
            model: model.into(),
            ..Route::default()
        };
        self.stream_route(&route, input).await
    }
    async fn complete_route(
        &self,
        route: &Route,
        input: &str,
        max_output: u32,
    ) -> Result<Completion> {
        // OpenRouter's `reasoning` is one request-level setting while chain members differ in
        // reasoning support (GLM cannot disable it), so the chain runs as sequential
        // single-model requests, each with its own effort.
        if let Some(chain) = self.chain(route) {
            let mut last = None;
            for sub in chain {
                match self.complete_route(&sub, input, max_output).await {
                    Ok(done) => return Ok(done),
                    Err(e) => last = Some(e),
                }
            }
            return Err(last.context("empty model chain")?);
        }
        let model = route.model.as_str();
        let value: Value = self
            .request(route, input, false, max_output)
            .send()
            .await?
            .error_for_status()?
            .json()
            .await?;
        if let Some(reason) = truncation_reason(&value, self.anthropic) {
            tracing::warn!(model, reason, "provider output truncated");
        }
        let (text, usage) = if self.anthropic {
            (&value["content"][0]["text"], &value["usage"])
        } else {
            (&value["choices"][0]["message"]["content"], &value["usage"])
        };
        // A null answer that reports usage still consumed tokens: keep it as an empty answer so
        // the bounded runner rejects and prices the attempt instead of losing its cost.
        let text = match text.as_str() {
            Some(t) => t.to_string(),
            None if usage.get("completion_tokens").is_some()
                || usage.get("output_tokens").is_some() =>
            {
                String::new()
            }
            None => anyhow::bail!("missing completion text"),
        };
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
            // Fallbacks change the serving model; billing follows the model that served.
            model: value["model"]
                .as_str()
                .filter(|m| !m.is_empty())
                .unwrap_or(model)
                .into(),
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
            output_tokens: billed_output(
                count(if self.anthropic {
                    "output_tokens"
                } else {
                    "completion_tokens"
                }),
                usage["completion_tokens_details"]["reasoning_tokens"]
                    .as_u64()
                    .and_then(|n| u32::try_from(n).ok())
                    .unwrap_or(0),
            ),
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
    async fn stream_route(&self, route: &Route, input: &str) -> Result<CompletionStream> {
        if let Some(chain) = self.chain(route) {
            let mut last = None;
            for sub in chain {
                match self.stream_route(&sub, input).await {
                    Ok(done) => return Ok(done),
                    Err(e) => last = Some(e),
                }
            }
            return Err(last.context("empty model chain")?);
        }
        let response = self
            .request(route, input, true, 4096)
            .send()
            .await?
            .error_for_status()?;
        let anthropic = self.anthropic;
        let model = route.model.clone();
        // Served model and reasoning tokens arrive in chunks; share them with the usage event.
        let served = std::sync::Arc::new(std::sync::Mutex::new((None::<String>, 0_u32)));
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
                let served = served.clone();
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
                                    if let Some(n) = count(
                                        &v["usage"]["completion_tokens_details"]["reasoning_tokens"],
                                    ) {
                                        served.lock().unwrap().1 = n;
                                    }
                                    if let Some(m) = v["model"].as_str().filter(|m| !m.is_empty()) {
                                        served.lock().unwrap().0 = Some(m.to_owned());
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
                                model: {
                                    let guard = served.lock().unwrap();
                                    guard.0.clone().unwrap_or(model)
                                },
                                input_tokens: if anthropic {
                                    input.unwrap_or(0)
                                } else {
                                    input.unwrap_or(0).saturating_sub(read)
                                },
                                output_tokens: billed_output(
                                    output.unwrap_or(0),
                                    served.lock().unwrap().1,
                                ),
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
/// Reasoning tokens bill as output. OpenAI-style `completion_tokens` already includes them;
/// if a provider reports more reasoning than completion tokens they must be reported separately.
fn billed_output(completion: u32, reasoning: u32) -> u32 {
    if reasoning > completion {
        completion.saturating_add(reasoning)
    } else {
        completion
    }
}
/// Name every routed provider answers to; the API quotes it (with the models.toml model ids).
pub const ROUTE_PROVIDER: &str = "openrouter";

/// Deterministic mock backend behind the production route identity: same provider name and the
/// same per-task model ids as live, so a quote the API builds for live is the quote mock mode
/// accepts. Only the transport differs.
pub struct RouteMock;
#[async_trait]
impl LlmProvider for RouteMock {
    fn name(&self) -> &'static str {
        ROUTE_PROVIDER
    }
    fn is_fixture(&self) -> bool {
        true
    }
    async fn complete(&self, model: &str, input: &str) -> Result<Completion> {
        let mut c = engine_core::MockProvider.complete(model, input).await?;
        c.provider = ROUTE_PROVIDER.into();
        Ok(c)
    }
    async fn stream(&self, model: &str, input: &str) -> Result<CompletionStream> {
        let inner = engine_core::MockProvider.stream(model, input).await?;
        Ok(Box::pin(inner.map(|event| {
            event.map(|e| match e {
                StreamEvent::Usage(mut u) => {
                    u.provider = ROUTE_PROVIDER.into();
                    StreamEvent::Usage(u)
                }
                other => other,
            })
        })))
    }
}

pub fn router(mode: &str) -> Result<engine_core::ProviderRouter> {
    use std::sync::Arc;
    if mode == "mock" {
        return engine_core::ProviderRouter::new(Arc::new(RouteMock), Arc::new(RouteMock));
    }
    if mode != "live" {
        bail!("unknown provider mode: '{mode}'");
    }
    let openrouter = std::env::var("OPENROUTER_API_KEY").context("OPENROUTER_API_KEY missing")?;
    if openrouter.is_empty() {
        bail!("OPENROUTER_API_KEY must not be empty");
    }
    let url = "https://openrouter.ai/api/v1/chat/completions";
    engine_core::ProviderRouter::new(
        Arc::new(HttpProvider::openrouter(openrouter.clone(), url.into())),
        Arc::new(HttpProvider::openrouter(openrouter, url.into())),
    )
}

#[cfg(test)]
mod truncation_tests {
    use super::*;

    #[test]
    fn identifies_provider_output_limits() {
        assert_eq!(
            truncation_reason(&json!({"choices":[{"finish_reason":"length"}]}), false),
            Some("finish_reason=length")
        );
        assert_eq!(
            truncation_reason(&json!({"stop_reason":"max_tokens"}), true),
            Some("stop_reason=max_tokens")
        );
        assert_eq!(
            truncation_reason(&json!({"choices":[{"finish_reason":"stop"}]}), false),
            None
        );
    }
}
