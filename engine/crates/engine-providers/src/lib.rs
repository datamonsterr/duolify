pub mod elevenlabs;
mod openrouter_gateway;
use anyhow::{Context, Result, bail};
use async_trait::async_trait;
use engine_core::{
    Completion, CompletionStream, GatewayBudget, GatewayCacheStatus, GatewayReason, LlmProvider,
    OpenRouterTransport, OpenRouterTransportError, Route, StreamEvent,
};
use futures::{StreamExt, stream};
pub use openrouter_gateway::GatewayConfig;
use openrouter_gateway::{Gateway, GatewayFailure};
use serde_json::{Value, json};
use std::{future::Future, time::Duration};

const CHAT_ENDPOINT: &str = "chat/completions";

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
    gateway: Option<Gateway>,
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
            gateway: None,
        }
    }
    pub fn openrouter(key: String, url: String) -> Self {
        Self {
            client: reqwest::Client::new(),
            url,
            key,
            anthropic: false,
            gateway: None,
        }
    }
    pub fn openrouter_with_gateway(
        key: String,
        direct_url: String,
        config: Option<GatewayConfig>,
    ) -> Result<Self> {
        let mut provider = Self::openrouter(key, direct_url);
        provider.gateway = config.map(Gateway::new).transpose()?;
        Ok(provider)
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
    fn chat_body(&self, route: &Route, input: &str, streaming: bool, max_output: u32) -> Value {
        let model = &route.model;
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
        if streaming && !self.anthropic {
            body["stream_options"] = json!({"include_usage":true});
        }
        body
    }

    async fn send(
        &self,
        request: reqwest::RequestBuilder,
        body: &Value,
    ) -> Result<reqwest::Response> {
        let request = if self.anthropic {
            request
                .header("x-api-key", &self.key)
                .header("anthropic-version", "2023-06-01")
        } else {
            request
                .bearer_auth(&self.key)
                .header("HTTP-Referer", "https://duolify.online")
                .header("X-Title", "Duolify")
        };
        Ok(request
            .json(body)
            .send()
            .await
            .map_err(reqwest::Error::without_url)?)
    }

    async fn routed<T, F, Fut>(
        &self,
        route: &Route,
        endpoint: &str,
        body: &Value,
        parse: F,
    ) -> Result<(T, Option<OpenRouterTransport>)>
    where
        F: Fn(reqwest::Response, bool) -> Fut,
        Fut: Future<Output = Result<T>>,
    {
        let chat = endpoint == CHAT_ENDPOINT;
        let direct = || async {
            let url = if chat {
                self.url.clone()
            } else {
                audio_url(&self.url, endpoint)
            };
            parse(
                check_status(self.send(self.client.post(url), body).await?)?,
                false,
            )
            .await
        };
        let Some(gateway) = self
            .gateway
            .as_ref()
            .filter(|gateway| chat || gateway.stt())
        else {
            let transport = (!self.anthropic).then_some(OpenRouterTransport::DIRECT);
            return with_transport(direct().await, transport);
        };
        let public = chat && route.gateway_cache_public;
        let budget = route.gateway_budget.unwrap_or(GatewayBudget {
            timeout: if chat {
                Duration::from_secs(30)
            } else {
                Duration::from_secs(8)
            },
            may_retry_billable: true,
        });
        let started = tokio::time::Instant::now();
        let gateway_window = budget.timeout / 2;
        let response = openrouter_gateway::try_gateway(
            gateway_window,
            self.send(gateway.request(endpoint, body, public), body),
        )
        .await;
        let result = match response {
            Ok(response) => {
                let remaining = gateway_window.saturating_sub(started.elapsed());
                let cache = openrouter_gateway::cache_status(&response, public);
                match openrouter_gateway::check_gateway_response(response, remaining).await {
                    Ok(response) => {
                        openrouter_gateway::try_gateway(remaining, async {
                            Ok((
                                parse(check_status(response)?, true).await?,
                                OpenRouterTransport::Gateway(cache),
                            ))
                        })
                        .await
                    }
                    Err(error) => Err((GatewayFailure::classify(&error), error)),
                }
            }
            Err(error) => Err(error),
        };
        match result {
            Ok((result, transport)) => Ok((result, Some(transport))),
            Err((failure, error)) => {
                let remaining = budget.timeout.saturating_sub(started.elapsed());
                if !remaining.is_zero()
                    && let Some(transport) = failure.and_then(|failure| failure.fallback(budget))
                {
                    let result = tokio::time::timeout(remaining, direct())
                        .await
                        .unwrap_or_else(|_| Err(anyhow::anyhow!("direct OpenRouter timeout")));
                    return with_transport(result, Some(transport));
                }
                let cache = if public {
                    GatewayCacheStatus::Unknown
                } else {
                    GatewayCacheStatus::Bypass
                };
                with_transport(Err(error), Some(OpenRouterTransport::Gateway(cache)))
            }
        }
    }
}

fn check_status(response: reqwest::Response) -> Result<reqwest::Response> {
    if !response.status().is_success() {
        return Err(engine_core::transcribe::HttpStatus(response.status().as_u16()).into());
    }
    Ok(response)
}

fn with_transport<T>(
    result: Result<T>,
    transport: Option<OpenRouterTransport>,
) -> Result<(T, Option<OpenRouterTransport>)> {
    result
        .map(|value| (value, transport))
        .map_err(|source| match transport {
            Some(transport) => OpenRouterTransportError { transport, source }.into(),
            None => source,
        })
}

async fn json_response(response: reqwest::Response, gateway: bool) -> Result<Value> {
    let value: Value = response.json().await.map_err(reqwest::Error::without_url)?;
    if gateway
        && let Some(reason) = openrouter_gateway::classify_response(reqwest::StatusCode::OK, &value)
    {
        return Err(GatewayFailure::after_send(reason).into());
    }
    Ok(value)
}

/// OpenRouter answers audio on its own path under the same API root as chat completions.
fn audio_url(chat_url: &str, endpoint: &str) -> String {
    let root = chat_url.find("/api/").map_or(chat_url, |i| &chat_url[..i]);
    format!("{}/api/v1/{endpoint}", root.trim_end_matches('/'))
}

/// A two or three letter lowercase code, else unknown: the value is logged and counted, so
/// provider free text never passes through.
fn language_code(value: &Value) -> String {
    value
        .as_str()
        .map(|l| l.trim().to_lowercase())
        .filter(|l| (2..=3).contains(&l.len()) && l.bytes().all(|b| b.is_ascii_lowercase()))
        .unwrap_or_default()
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
        let body = self.chat_body(route, input, false, max_output);
        let (value, transport) = self
            .routed(route, CHAT_ENDPOINT, &body, json_response)
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
            transport,
        })
    }
    async fn stream_route(&self, route: &Route, input: &str) -> Result<CompletionStream> {
        Ok(self
            .stream_route_with_transport(route, input, 4096)
            .await?
            .0)
    }
    async fn stream_route_with_transport(
        &self,
        route: &Route,
        input: &str,
        max_output: u32,
    ) -> Result<(CompletionStream, Option<OpenRouterTransport>)> {
        if let Some(chain) = self.chain(route) {
            let mut last = None;
            for sub in chain {
                match self
                    .stream_route_with_transport(&sub, input, max_output)
                    .await
                {
                    Ok(done) => return Ok(done),
                    Err(e) => last = Some(e),
                }
            }
            return Err(last.context("empty model chain")?);
        }
        let body = self.chat_body(route, input, true, max_output);
        let (stream, transport) = self
            .routed(
                route,
                CHAT_ENDPOINT,
                &body,
                |response, via_gateway| async move {
                    if via_gateway
                        && response
                            .headers()
                            .get(reqwest::header::CONTENT_TYPE)
                            .and_then(|value| value.to_str().ok())
                            .is_some_and(|value| value.starts_with("application/json"))
                    {
                        json_response(response, true).await?;
                        bail!("gateway returned JSON in place of a provider stream");
                    }
                    let mut stream =
                        response_stream(response, self.anthropic, route.model.clone(), via_gateway);
                    if self.gateway.is_none() {
                        return Ok(stream);
                    }
                    // Inspect only the first visible event. Once returned, this stream has no retry path.
                    let first = stream
                        .next()
                        .await
                        .context("provider stream ended before completion")??;
                    Ok(Box::pin(stream::once(async { Ok(first) }).chain(stream))
                        as CompletionStream)
                },
            )
            .await?;
        Ok((
            Box::pin(stream.map(move |event| {
                event.map(|event| match event {
                    StreamEvent::Usage(mut usage) => {
                        usage.transport = transport;
                        StreamEvent::Usage(usage)
                    }
                    other => other,
                })
            })),
            transport,
        ))
    }

    /// OpenRouter `POST /api/v1/audio/transcriptions`: base64 audio and the ADR 0021 policy.
    async fn transcribe(
        &self,
        route: &Route,
        model: &str,
        audio: &[u8],
        container: &str,
        language: Option<&str>,
    ) -> Result<engine_core::transcribe::Transcription> {
        use base64::Engine as _;
        if self.anthropic {
            bail!("the direct Anthropic route has no transcription endpoint");
        }
        let endpoint = route
            .endpoint
            .as_deref()
            .and_then(|endpoint| endpoint.strip_prefix("/api/v1/"))
            .filter(|endpoint| *endpoint == "audio/transcriptions")
            .context("transcription route has no supported endpoint")?;
        anyhow::ensure!(
            route.zdr && route.data_collection.as_deref() == Some("deny"),
            "transcription route must require zero data retention"
        );
        let mut body = json!({
            "model": model,
            "input_audio": {
                "data": base64::engine::general_purpose::STANDARD.encode(audio),
                "format": container,
            },
            "response_format": "verbose_json",
            "temperature": 0,
            "provider": {"data_collection": "deny", "zdr": true},
        });
        if let Some(language) = language {
            body["language"] = json!(language);
        }
        let (value, transport) = self.routed(route, endpoint, &body, json_response).await?;
        let text = value["text"]
            .as_str()
            .context("transcription response has no text")?
            .to_string();
        let usage = &value["usage"];
        let cost = usage["cost"]
            .as_f64()
            .filter(|c| c.is_finite() && *c >= 0.0);
        Ok(engine_core::transcribe::Transcription {
            text,
            language: language_code(&value["language"]),
            seconds: usage["seconds"].as_f64().unwrap_or_default(),
            cost_nanos: cost.map_or(0, |c| (c * 1e9).round() as u64),
            cost_reported: cost.is_some(),
            confidence: value["confidence"]
                .as_f64()
                .filter(|c| c.is_finite() && (0.0..=1.0).contains(c)),
            model: model.into(),
            transport,
        })
    }
}

fn response_stream(
    response: reqwest::Response,
    anthropic: bool,
    model: String,
    via_gateway: bool,
) -> CompletionStream {
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
                                        (bytes, buffer, true, input, output, write, read, emitted),
                                    ));
                                }
                            };
                            if v["type"] == "error"
                                || !v["error"].is_null()
                                || v["errors"]
                                    .as_array()
                                    .is_some_and(|errors| !errors.is_empty())
                            {
                                let error = if via_gateway
                                    && let Some(reason) = openrouter_gateway::classify_response(
                                        reqwest::StatusCode::OK,
                                        &v,
                                    ) {
                                    GatewayFailure::after_send(reason).into()
                                } else {
                                    anyhow::anyhow!("provider stream error")
                                };
                                return Some((
                                    Err(error),
                                    (bytes, buffer, true, input, output, write, read, true),
                                ));
                            }
                            let count = |v: &Value| v.as_u64().and_then(|n| u32::try_from(n).ok());
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
                            if let Some(text) = delta.filter(|text| !text.is_empty()) {
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
                            transport: None,
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
                                    Err(e.without_url().into()),
                                    (bytes, buffer, true, input, output, write, read, true),
                                ));
                            }
                            None => {
                                let error = if via_gateway {
                                    GatewayFailure::after_send(GatewayReason::TransportError).into()
                                } else {
                                    anyhow::anyhow!("provider stream ended before completion")
                                };
                                return Some((
                                    Err(error),
                                    (bytes, Vec::new(), true, input, output, write, read, true),
                                ));
                            }
                        }
                    }
                }
            }
        },
    );
    Box::pin(output)
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
    async fn transcribe(
        &self,
        route: &Route,
        model: &str,
        audio: &[u8],
        container: &str,
        language: Option<&str>,
    ) -> Result<engine_core::transcribe::Transcription> {
        engine_core::MockProvider
            .transcribe(route, model, audio, container, language)
            .await
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
    let read_setting = |name| match std::env::var(name) {
        Ok(value) => Ok(Some(value)),
        Err(std::env::VarError::NotPresent) => Ok(None),
        Err(std::env::VarError::NotUnicode(_)) => bail!("invalid UTF-8 in {name}"),
    };
    let base = read_setting("OPENROUTER_BASE_URL")?;
    let token = read_setting("AI_GATEWAY_TOKEN")?;
    let stt = read_setting("AI_GATEWAY_STT_ENABLED")?;
    let gateway = GatewayConfig::parse(base.as_deref(), token.as_deref(), stt.as_deref())?;
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
    let url = format!("{}/{CHAT_ENDPOINT}", openrouter_gateway::DIRECT_BASE_URL);
    let provider = Arc::new(HttpProvider::openrouter_with_gateway(
        openrouter, url, gateway,
    )?);
    engine_core::ProviderRouter::new(provider.clone(), provider)
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
