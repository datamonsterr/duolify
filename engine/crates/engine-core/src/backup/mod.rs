//! Safe operational correlation shared by the engine's RPC boundary.
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

/// Engine service configuration in an infrastructure environment plan.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct EngineServiceConfig {
    pub listen_addr: String,
    pub domains: Vec<String>,
    pub env_vars: std::collections::HashMap<String, String>,
}

/// Infrastructure plan for an environment including dependencies and engine config.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct EngineEnvironmentPlan {
    pub environment: String,
    pub project_name: String,
    pub engine_service: EngineServiceConfig,
    pub has_database: bool,
    pub has_redis: bool,
    pub has_assets_bucket: bool,
    pub has_backups_bucket: bool,
}

/// Validation result of an environment plan.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct PlanValidationResult {
    pub is_valid: bool,
    pub violations: Vec<String>,
    pub plan_summary: String,
    pub live_apply_allowed: bool,
    pub hitl_gate_required: String,
}

impl EngineEnvironmentPlan {
    /// Validates that:
    /// 1. Environment is one of "dev", "staging", "prod" and project is "duolify".
    /// 2. Engine has no public domains (domains must be empty).
    /// 3. Dependencies are present (Postgres, Redis, assets and backups buckets).
    /// 4. No plaintext secrets are committed in config (values must use preserve() or references).
    /// 5. Live apply is disabled and gated on E11-07.
    pub fn validate(&self) -> PlanValidationResult {
        let mut violations = Vec::new();

        // 1. Environment and project check
        if !["dev", "staging", "prod"].contains(&self.environment.as_str()) {
            violations.push(format!(
                "invalid environment {:?}; allowed environments are dev, staging, prod",
                self.environment
            ));
        }
        if self.project_name != "duolify" {
            violations.push(format!(
                "invalid project_name {:?}; expected duolify",
                self.project_name
            ));
        }

        // 2. Private engine check: engine must NEVER have public domains
        if !self.engine_service.domains.is_empty() {
            violations.push(format!(
                "engine service must not define public domains; found domains: {:?}",
                self.engine_service.domains
            ));
        }

        // 3. Dependencies check
        if !self.has_database {
            violations.push("missing required database dependency (postgres 18)".to_string());
        }
        if !self.has_redis {
            violations.push("missing required cache dependency (redis)".to_string());
        }
        if !self.has_assets_bucket {
            violations
                .push("missing required storage bucket dependency (lesson-assets)".to_string());
        }
        if !self.has_backups_bucket {
            violations.push("missing required storage bucket dependency (backups)".to_string());
        }

        // 4. Plaintext secret detection
        for (key, val) in &self.engine_service.env_vars {
            let val_lower = val.trim();
            // Check if key looks like a secret
            let is_secret_key = key.contains("KEY")
                || key.contains("SECRET")
                || key.contains("TOKEN")
                || key.contains("PASSWORD");

            if is_secret_key {
                let is_safe_ref = val_lower == "preserve()"
                    || (val_lower.starts_with("${{") && val_lower.ends_with("}}"))
                    || val_lower.is_empty();

                if !is_safe_ref {
                    violations.push(format!(
                        "secret env var {} has plaintext value instead of preserve() or reference",
                        key
                    ));
                }
            } else if val_lower.starts_with("sk-ant-")
                || val_lower.starts_with("sk-or-")
                || val_lower.starts_with("eyJ")
            {
                violations.push(format!(
                    "env var {} contains plaintext secret value pattern",
                    key
                ));
            }
        }

        let is_valid = violations.is_empty();
        let plan_summary = format!(
            "Environment: {}, Project: {}, Engine domains: {:?}, Database: {}, Redis: {}, Assets: {}, Backups: {}",
            self.environment,
            self.project_name,
            self.engine_service.domains,
            self.has_database,
            self.has_redis,
            self.has_assets_bucket,
            self.has_backups_bucket
        );

        PlanValidationResult {
            is_valid,
            violations: violations.into_iter().map(|v| redact_secrets(&v)).collect(),
            plan_summary: redact_secrets(&plan_summary),
            live_apply_allowed: false, // Always false in AFK ticket
            hitl_gate_required: "E11-07".to_string(),
        }
    }
}

#[cfg(test)]
mod tests;
