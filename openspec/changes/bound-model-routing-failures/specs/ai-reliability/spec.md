## MODIFIED Requirements

### Requirement: Provider fallback
The Engine SHALL use one `LlmProvider` trait with Claude API and OpenRouter implementations plus a mock. At most one bounded repair or fallback attempt SHALL follow the primary attempt, subject to attempt count and USD cost ceilings. If both primary and fallback fail, or if fallback would exceed quote or provider USD caps, work MUST stop with a typed retryable error and the API MUST release undelivered reserved Credits. Model IDs MUST come from `engine/config/models.toml` and verified canonical decimal OpenRouter aliases. Streaming calls MUST NOT fall back after emitting any delta event to the client.

#### Scenario: Primary outage
- **WHEN** the primary provider returns 5xx or 429
- **THEN** the router calls the fallback if within attempt and USD limits, reporting the fallback provider

#### Scenario: Both providers fail
- **WHEN** both providers fail or fallback exceeds budget
- **THEN** the Engine returns a typed retryable error, the API releases undelivered Credits, and the client displays a retry option

#### Scenario: Stream failure after delta
- **WHEN** a provider stream fails after emitting at least one delta
- **THEN** streaming stops with an error and no fallback provider is called

### Requirement: Timeouts
Each task MUST enforce a task timeout, and streaming tasks MUST enforce both a first-token timeout and an idle timeout. Exceeding any timeout MUST cancel the in-flight provider call, record the attempt as a failed attempt with usage accounting, and not deliver partial unsafe output.

#### Scenario: First token timeout
- **WHEN** the first token is not received within the first-token timeout
- **THEN** the primary attempt is cancelled and may fall back to the secondary route if within limits

#### Scenario: Stream idle timeout
- **WHEN** the interval between consecutive stream tokens exceeds the idle timeout
- **THEN** the stream is terminated with a timeout error and cancelled without post-delta fallback
