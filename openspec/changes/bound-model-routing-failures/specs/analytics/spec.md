## MODIFIED Requirements

### Requirement: LLM analytics
Every Engine call SHALL produce a PostHog `$ai_generation` event from the API with `$ai_provider`, `$ai_model`, `$ai_input_tokens`, `$ai_output_tokens`, `$ai_latency`, `feature`, `fallback_used`, `$ai_is_error`, and sanitized `error_code`. Error codes MUST be bounded identifier tokens (such as `rate_limited`, `provider_outage`, `invalid_schema`, `timeout`, `expensive_fallback`, `cancelled`, `stream_error`) and MUST NOT contain raw provider diagnostics, prompt text, or completion content.

#### Scenario: Fallback call
- **WHEN** the Engine used the fallback provider
- **THEN** `$ai_generation` has `fallback_used` true and the real provider and model

#### Scenario: Provider error
- **WHEN** a provider call fails with an outage or timeout
- **THEN** `$ai_generation` records `$ai_is_error` true and the sanitized `error_code` without free text
