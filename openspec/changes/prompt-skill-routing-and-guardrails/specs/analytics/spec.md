## ADDED Requirements

### Requirement: Prompt routing and guard properties
Engine usage reported as PostHog `$ai_generation` SHALL add `prompt_mode` (`monolithic` or `routed`), `skills_loaded` (array of skill ids, sorted), `skills_version` (integer), `router_source` (`rules`, `cache`, `model`, `default` or `none`), `router_model`, `router_latency_ms`, `router_cost_usd`, `guardrail_verdict` (`allowed`, `blocked`, `self_harm`, `timeout`, `skipped`), `guardrail_reason_code` (existing enum or empty), `guardrail_latency_ms`, `guardrail_cost_usd` and `cached_input_tokens`. Properties SHALL contain ids, enums and numbers only, never learner text, router input or guard input. Dashboards SHALL chart router source share, default-set rate, guard block rate by surface, guard timeout rate, cache-hit share and cost per task by `prompt_mode`.

#### Scenario: Routed lesson event
- **WHEN** a routed lesson generation completes
- **THEN** its `$ai_generation` event carries `skills_loaded`, `router_source` and `cached_input_tokens` and no text from the lesson request

#### Scenario: Opt-out
- **WHEN** the learner has opted out of analytics
- **THEN** none of these properties are sent, as for every other event
