## ADDED Requirements

### Requirement: Cloudflare offload telemetry
Cloudflare offload analytics SHALL use the existing consent-aware PostHog wrappers, common properties, bounded enums and eligible UUID identities. Add `edge_public_read` (`surface`, `read_path`, `cache_status`, `outcome`), `turnstile_verification` (`surface`, `outcome`, `reason_code`) and `chat_transport_fallback` (`surface`, `from_transport`, `to_transport`, `reason_code`) to the API allowlist and coverage catalogue before emission. Add `transport_route`, `gateway_fallback`, `gateway_cache_status` and `gateway_reason` to applicable `$ai_generation` events. Applicable narration events and narration `$ai_generation` MUST identify `provider`, `model`, `timing_source`, `fallback_used`, and, for Workers AI usage, `workers_ai_neurons`, with bounded `reason_code` for failures. Existing Tutor chat terminal and applicable Guided chat network events SHALL identify `chat_transport`. Canonical finance and model telemetry MUST preserve one settlement and actual-attempt attribution. Anonymous traffic without consented identity SHALL produce aggregate operational counts only. Prompts, replies, narration text, audio, transcripts, raw diagnostics, raw query strings, credentials and link/challenge tokens MUST be excluded.

#### Scenario: Gateway fallback and opt-out
- **WHEN** a consented Learner's request falls back from the Gateway to direct OpenRouter
- **THEN** the API records only model usage and bounded route metadata, and suppresses delivery for an opted-out Learner

#### Scenario: Anonymous challenge
- **WHEN** a share-preview challenge completes without a consented identity
- **THEN** only an aggregate operational counter can be recorded and no PostHog event carries the proof or share token
