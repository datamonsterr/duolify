## ADDED Requirements

### Requirement: OpenRouter transport through AI Gateway
The Engine SHALL accept an OpenRouter base URL with the direct endpoint as the default and authenticate AI Gateway requests with an account-scoped gateway token separate from the OpenRouter credential. It MUST preserve provider policy, validation, budgets and timeout behavior on both routes. AI Gateway logging and cache MUST be disabled per request for Learner content, voice input and unclassified traffic. Only server-approved, identical non-personal shared demo or public preview inference MAY opt into a bounded cache. The gateway MUST use existing provider credentials and opt out of Cloudflare Unified Billing fallback.

#### Scenario: Personal request
- **WHEN** the Engine routes a Tutor chat, private generation or transcription request through the Gateway
- **THEN** the request has log collection disabled and skips cache, and contains neither Gateway billing fallback nor caller-controlled cache eligibility

#### Scenario: Identical approved public input
- **WHEN** two eligible non-personal requests have exactly equivalent versioned inputs
- **THEN** they may reuse a cached result within the configured TTL, and different or unclassified inputs cannot use that entry

### Requirement: Bounded direct OpenRouter fallback
On a transient Gateway/transport failure before valid delivery, the Engine SHALL try direct OpenRouter once within the original deadline and budget. The direct attempt MUST retain provider privacy flags and exclude Cloudflare auth and cache headers. An intentional policy, quota or validation denial SHALL retain its original meaning. On cancellation or after a delivered stream delta the Engine MUST stop without replay. Existing model fallback and its Golden set gates SHALL apply independently within the same bounded attempt envelope.

#### Scenario: Gateway unavailable before first delta
- **WHEN** the Gateway times out before any valid output and the direct route remains eligible
- **THEN** the Engine tries the direct OpenRouter endpoint once and records the fallback without content or credentials

#### Scenario: Gateway fails mid-stream
- **WHEN** the Gateway disconnects after the first delivered delta
- **THEN** the existing partial-output settlement and recovery contract applies and no direct call starts
