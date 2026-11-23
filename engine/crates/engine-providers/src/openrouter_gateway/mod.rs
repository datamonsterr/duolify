use anyhow::{Context, Result, bail, ensure};
use engine_core::{GatewayBudget, GatewayCacheStatus, GatewayReason, OpenRouterTransport};
use reqwest::{Client, RequestBuilder, Response, StatusCode, Url, header::HeaderValue};
use serde_json::Value;
use std::{future::Future, time::Duration};

#[cfg(test)]
mod tests;

pub(crate) const DIRECT_BASE_URL: &str = "https://openrouter.ai/api/v1";
const PUBLIC_CACHE_TTL: &str = "300";
const CACHE_KEY_VERSION: &[u8] = b"duolify:openrouter:1\0";

pub struct GatewayConfig {
    base: Url,
    authorization: Option<HeaderValue>,
    pub(crate) stt: bool,
}

impl GatewayConfig {
    pub fn parse(
        url: Option<&str>,
        token: Option<&str>,
        stt: Option<&str>,
    ) -> Result<Option<Self>> {
        let stt = match stt {
            None | Some("" | "false") => false,
            Some("true") => true,
            Some(_) => bail!("AI_GATEWAY_STT_ENABLED must be true or false"),
        };
        let raw_url = url.unwrap_or_default();
        let url = raw_url.trim_end_matches('/');
        let token = token.filter(|token| !token.is_empty());
        if raw_url.is_empty() || url == DIRECT_BASE_URL {
            ensure!(
                token.is_none() && !stt,
                "Gateway options require a Gateway OPENROUTER_BASE_URL"
            );
            return Ok(None);
        }
        let base = Url::parse(url).context("invalid OPENROUTER_BASE_URL")?;
        let local = matches!(base.host_str(), Some("localhost" | "127.0.0.1" | "[::1]"));
        ensure!(
            (base.scheme() == "https" || (local && base.scheme() == "http"))
                && base.has_host()
                && base.host_str() != Some("openrouter.ai")
                && base.username().is_empty()
                && base.password().is_none()
                && base.query().is_none()
                && base.fragment().is_none(),
            "invalid OPENROUTER_BASE_URL: use a Gateway HTTPS base without credentials, query or fragment"
        );
        if base.host_str() == Some("gateway.ai.cloudflare.com") {
            let segments: Vec<_> = base.path_segments().into_iter().flatten().collect();
            ensure!(
                matches!(segments.as_slice(), ["v1", account, gateway, "openrouter"] if !account.is_empty() && !gateway.is_empty()),
                "invalid OPENROUTER_BASE_URL: expected /v1/<account>/<gateway>/openrouter"
            );
        }
        let authorization = token
            .map(|token| {
                ensure!(
                    token.bytes().all(|byte| byte.is_ascii_graphic()),
                    "invalid AI_GATEWAY_TOKEN"
                );
                let mut value = HeaderValue::from_str(&format!("Bearer {token}"))
                    .context("invalid AI_GATEWAY_TOKEN")?;
                value.set_sensitive(true);
                Ok::<_, anyhow::Error>(value)
            })
            .transpose()?;
        Ok(Some(Self {
            base,
            authorization,
            stt,
        }))
    }

    pub(crate) fn url(&self, endpoint: &str) -> Url {
        let mut url = self.base.clone();
        url.path_segments_mut()
            .expect("validated HTTP base URL")
            .pop_if_empty()
            .extend(endpoint.split('/'));
        url
    }
}

pub(crate) struct Gateway {
    config: GatewayConfig,
    client: Client,
}

impl Gateway {
    pub(crate) fn new(config: GatewayConfig) -> Result<Self> {
        let client = Client::builder()
            .redirect(reqwest::redirect::Policy::none())
            .retry(reqwest::retry::never())
            .build()?;
        Ok(Self { config, client })
    }

    pub(crate) fn stt(&self) -> bool {
        self.config.stt
    }

    pub(crate) fn request(&self, endpoint: &str, body: &Value, public: bool) -> RequestBuilder {
        let url = self.config.url(endpoint);
        let mut request = self
            .client
            .post(url.clone())
            .header("cf-aig-collect-log", "false")
            .header("cf-aig-max-attempts", "1")
            .header("cf-aig-no-wholesale", "true");
        if let Some(authorization) = &self.config.authorization {
            request = request.header("cf-aig-authorization", authorization);
        }
        if public {
            request
                .header("cf-aig-cache-key", cache_key(&url, body))
                .header("cf-aig-cache-ttl", PUBLIC_CACHE_TTL)
        } else {
            request.header("cf-aig-skip-cache", "true")
        }
    }
}

fn cache_key(url: &Url, body: &Value) -> String {
    use std::fmt::Write as _;
    let mut digest = ring::digest::Context::new(&ring::digest::SHA256);
    digest.update(CACHE_KEY_VERSION);
    digest.update(url.as_str().as_bytes());
    digest.update(b"\0");
    digest.update(body.to_string().as_bytes());
    let mut key = String::with_capacity(64);
    for byte in digest.finish().as_ref() {
        write!(key, "{byte:02x}").expect("writing to String cannot fail");
    }
    key
}

pub(crate) fn cache_status(response: &Response, public: bool) -> GatewayCacheStatus {
    if !public {
        return GatewayCacheStatus::Bypass;
    }
    match response
        .headers()
        .get("cf-aig-cache-status")
        .and_then(|value| value.to_str().ok())
    {
        Some("HIT") => GatewayCacheStatus::Hit,
        Some("MISS") => GatewayCacheStatus::Miss,
        _ => GatewayCacheStatus::Unknown,
    }
}

pub(crate) fn classify_response(status: StatusCode, body: &Value) -> Option<GatewayReason> {
    if status == StatusCode::GATEWAY_TIMEOUT {
        return Some(GatewayReason::Timeout);
    }
    if status.is_server_error() {
        return Some(GatewayReason::Unavailable);
    }
    let unavailable = |error: &Value| error["code"].as_u64() == Some(2005);
    if (status == StatusCode::BAD_REQUEST || status.is_success())
        && (unavailable(&body["error"])
            || body["errors"]
                .as_array()
                .is_some_and(|errors| errors.iter().any(unavailable)))
    {
        return Some(GatewayReason::Unavailable);
    }
    None
}

#[derive(Debug, Clone, Copy)]
pub(crate) struct GatewayFailure {
    reason: GatewayReason,
    may_bill: bool,
}

impl GatewayFailure {
    pub(crate) fn after_send(reason: GatewayReason) -> Self {
        Self {
            reason,
            may_bill: true,
        }
    }

    pub(crate) fn classify(error: &anyhow::Error) -> Option<Self> {
        if let Some(failure) = error.downcast_ref::<Self>() {
            return Some(*failure);
        }
        let error = error.downcast_ref::<reqwest::Error>()?;
        if error.is_connect() || error.is_dns() {
            return Some(Self {
                reason: if error.is_timeout() {
                    GatewayReason::Timeout
                } else {
                    GatewayReason::TransportError
                },
                may_bill: false,
            });
        }
        if error.is_timeout() {
            return Some(Self::after_send(GatewayReason::Timeout));
        }
        if error.is_body() || (error.is_request() && !error.is_builder()) {
            return Some(Self::after_send(GatewayReason::TransportError));
        }
        None
    }

    pub(crate) fn fallback(self, budget: GatewayBudget) -> Option<OpenRouterTransport> {
        if self.may_bill && !budget.may_retry_billable {
            return None;
        }
        let reason = match self.reason {
            GatewayReason::Timeout => "timeout",
            GatewayReason::Unavailable => "unavailable",
            GatewayReason::TransportError => "transport_error",
        };
        tracing::warn!(
            event = "openrouter_gateway_fallback",
            provider = crate::ROUTE_PROVIDER,
            transport_route = "direct",
            gateway_cache_status = "bypass",
            gateway_fallback = true,
            gateway_reason = reason,
            "retrying OpenRouter directly"
        );
        Some(OpenRouterTransport::Direct {
            fallback_reason: Some(self.reason),
            gateway_may_bill: self.may_bill,
        })
    }
}

impl std::fmt::Display for GatewayFailure {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(match self.reason {
            GatewayReason::Timeout => "gateway timeout",
            GatewayReason::Unavailable => "gateway service unavailable",
            GatewayReason::TransportError => "gateway transport error",
        })
    }
}

impl std::error::Error for GatewayFailure {}

pub(crate) async fn try_gateway<T>(
    timeout: Duration,
    operation: impl Future<Output = Result<T>>,
) -> std::result::Result<T, (Option<GatewayFailure>, anyhow::Error)> {
    match tokio::time::timeout(timeout, operation).await {
        Ok(Ok(result)) => Ok(result),
        Ok(Err(error)) => Err((GatewayFailure::classify(&error), error)),
        Err(_) => {
            let failure = GatewayFailure::after_send(GatewayReason::Timeout);
            Err((Some(failure), failure.into()))
        }
    }
}

pub(crate) async fn check_gateway_response(
    response: Response,
    remaining: Duration,
) -> Result<Response> {
    let status = response.status();
    if let Some(reason) = classify_response(status, &Value::Null) {
        return Err(GatewayFailure::after_send(reason).into());
    }
    if status == StatusCode::BAD_REQUEST {
        // Only the numeric code can change retry policy. Discard the diagnostic body.
        let body = tokio::time::timeout(remaining, response.json())
            .await
            .ok()
            .and_then(Result::ok)
            .unwrap_or(Value::Null);
        if let Some(reason) = classify_response(status, &body) {
            return Err(GatewayFailure::after_send(reason).into());
        }
        return Err(engine_core::transcribe::HttpStatus(status.as_u16()).into());
    }
    Ok(response)
}
