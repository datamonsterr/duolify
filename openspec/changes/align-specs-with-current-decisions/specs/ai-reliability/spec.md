## MODIFIED Requirements

### Requirement: Provider fallback
The Engine SHALL use one `LlmProvider` trait with OpenRouter for live requests and a mock for tests. Every live task SHALL route through OpenRouter using `tencent/hy4-flash` as primary and `z-ai/glm-5.3-flash` as fallback; direct Anthropic is not a live route. At most one bounded repair or fallback attempt SHALL follow the primary attempt, subject to attempt count and USD cost ceilings. If both primary and fallback fail, or if fallback would exceed quote or provider USD caps, work MUST stop with a typed retryable error and the API MUST release undelivered reserved Credits. Model IDs MUST come from `engine/config/models.toml` and verified canonical decimal OpenRouter aliases. Streaming calls MUST NOT fall back after emitting any delta event to the client.

#### Scenario: Primary outage
- **WHEN** the primary provider returns 5xx or 429
- **THEN** the router calls the fallback if within attempt and USD limits, reporting the fallback provider

#### Scenario: Both providers fail
- **WHEN** both providers fail or fallback exceeds budget
- **THEN** the Engine returns a typed retryable error, the API releases undelivered Credits, and the client displays a retry option

#### Scenario: Stream failure after delta
- **WHEN** a provider stream fails after emitting at least one delta
- **THEN** streaming stops with an error and no fallback provider is called
