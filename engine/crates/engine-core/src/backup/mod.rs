//! Safe operational correlation and engine configuration release gates.
//! This module has no backup or model behavior; metadata never contains payloads.

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Correlation {
    pub request_id: String,
    pub trace_id: String,
    pub parent_span_id: String,
}

impl Correlation {
    pub fn parse(request_id: &str, traceparent: &str) -> Option<Self> {
        let valid_hex = |value: &str, size| {
            value.len() == size
                && value
                    .bytes()
                    .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
                && value.bytes().any(|b| b != b'0')
        };
        if request_id.len() != 26
            || !request_id
                .bytes()
                .all(|b| b.is_ascii_uppercase() || (b'2'..=b'7').contains(&b))
        {
            return None;
        }
        let parts: Vec<_> = traceparent.split('-').collect();
        if parts.len() != 4
            || parts[0] != "00"
            || !valid_hex(parts[1], 32)
            || !valid_hex(parts[2], 16)
            || parts[3].len() != 2
            || !parts[3].bytes().all(|b| b.is_ascii_hexdigit())
        {
            return None;
        }
        Some(Self {
            request_id: request_id.into(),
            trace_id: parts[1].into(),
            parent_span_id: parts[2].into(),
        })
    }
}

/// Redact secrets such as API keys, tokens, and authorization values in text.
pub fn redact_secrets(input: &str) -> String {
    let mut result = input.to_string();

    // Redact bearer tokens
    if let Ok(re) = regex::Regex::new(r"(?i)bearer\s+[A-Za-z0-9_\-\.]+") {
        result = re.replace_all(&result, "Bearer [REDACTED]").to_string();
    }
    // Redact Anthropic API keys (sk-ant-...)
    if let Ok(re) = regex::Regex::new(r"sk-ant-[A-Za-z0-9_\-]+") {
        result = re.replace_all(&result, "[REDACTED]").to_string();
    }
    // Redact OpenRouter API keys (sk-or-...)
    if let Ok(re) = regex::Regex::new(r"sk-or-[A-Za-z0-9_\-]+") {
        result = re.replace_all(&result, "[REDACTED]").to_string();
    }
    // Redact JWT tokens (eyJ...)
    if let Ok(re) = regex::Regex::new(r"eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+") {
        result = re.replace_all(&result, "[REDACTED]").to_string();
    }
    // Redact connection URLs with passwords: postgres://user:password@host
    if let Ok(re) = regex::Regex::new(r"(?i)(postgres(?:ql)?|redis)://([^:]+):([^@]+)@") {
        result = re.replace_all(&result, "$1://$2:[REDACTED]@").to_string();
    }

    result
}

/// Engine startup configuration for release gate and private networking validation.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct EngineStartupConfig {
    pub duolify_env: String,
    pub listen_addr: String,
    pub provider_mode: String,
    pub public_domain: Option<String>,
    pub anthropic_api_key: Option<String>,
    pub openrouter_api_key: Option<String>,
}

impl EngineStartupConfig {
    pub fn from_env() -> Self {
        Self {
            duolify_env: std::env::var("DUOLIFY_ENV").unwrap_or_else(|_| "development".to_string()),
            listen_addr: std::env::var("ENGINE_ADDR_LISTEN")
                .unwrap_or_else(|_| "0.0.0.0:50051".to_string()),
            provider_mode: std::env::var("ENGINE_PROVIDER_MODE").unwrap_or_default(),
            public_domain: std::env::var("RAILWAY_PUBLIC_DOMAIN")
                .ok()
                .or_else(|| std::env::var("PUBLIC_DOMAIN").ok()),
            anthropic_api_key: std::env::var("ANTHROPIC_API_KEY")
                .ok()
                .filter(|s| !s.trim().is_empty()),
            openrouter_api_key: std::env::var("OPENROUTER_API_KEY")
                .ok()
                .filter(|s| !s.trim().is_empty()),
        }
    }

    /// Validates engine configuration against environment release gates.
    ///
    /// Release gate requirements:
    /// 1. The AI engine must NEVER be exposed via public domains or ingress.
    ///    In staging and production, any public domain configuration causes immediate refusal to start.
    /// 2. In staging and production (`DUOLIFY_ENV` in ["staging", "production", "prod"]):
    ///    - If `ENGINE_PROVIDER_MODE` is "live", at least one LLM credential
    ///      (`ANTHROPIC_API_KEY` or `OPENROUTER_API_KEY`) is required.
    ///    - The listen address must be a valid socket address.
    pub fn validate(&self) -> Result<(), EngineConfigError> {
        // Parse listen address
        if self.listen_addr.parse::<std::net::SocketAddr>().is_err() {
            return Err(EngineConfigError::InvalidAddress(format!(
                "invalid ENGINE_ADDR_LISTEN: {:?}",
                self.listen_addr
            )));
        }

        let is_prod_or_staging = matches!(
            self.duolify_env.to_lowercase().as_str(),
            "staging" | "production" | "prod"
        );

        if is_prod_or_staging {
            // Gate 1: Engine private networking only.
            if let Some(domain) = self.public_domain.as_ref().filter(|d| !d.trim().is_empty()) {
                return Err(EngineConfigError::PublicExposure(format!(
                    "engine must never have public domain; detected: {}",
                    redact_secrets(domain)
                )));
            }

            // Gate 2: Required provider credentials in live provider mode.
            if self.provider_mode == "live" {
                let has_llm_key =
                    self.anthropic_api_key.is_some() || self.openrouter_api_key.is_some();
                if !has_llm_key {
                    return Err(EngineConfigError::MissingDependency(
                        "production live engine requires ANTHROPIC_API_KEY or OPENROUTER_API_KEY"
                            .to_string(),
                    ));
                }
            }
        }

        Ok(())
    }
}

#[derive(Debug, PartialEq, Eq)]
pub enum EngineConfigError {
    PublicExposure(String),
    MissingDependency(String),
    InvalidAddress(String),
}

impl std::fmt::Display for EngineConfigError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::PublicExposure(msg) => write!(f, "engine exposed publicly: {}", msg),
            Self::MissingDependency(msg) => {
                write!(f, "missing required dependency or credential: {}", msg)
            }
            Self::InvalidAddress(msg) => write!(f, "invalid listen address: {}", msg),
        }
    }
}

impl std::error::Error for EngineConfigError {}
