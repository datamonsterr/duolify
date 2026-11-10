## MODIFIED Requirements

### Requirement: Variant generation and exhausted pools
The Engine SHALL generate a pool of `variants_per_topic` (default 2, config) Retry variants for every topic together with the Lesson, so that initial corrections require no additional network call. When the pool for a topic is exhausted, unavailable, or invalid, the API MUST reuse the pool or source Question with reordered choices for free without an Engine or provider call, keeping tests usable at zero balance. On-demand generation failures and pool exhaustion MUST emit `retry_variant_generation_failed` with the topic ID and error code, and MUST emit `retry_variant_served` without learner text. The API MUST retain the lesson instruction snapshot, enforce recap limits, and never auto-pass any topic.

#### Scenario: Pool exhausted free reuse
- **WHEN** a Learner answers wrongly on topic T and all pre-generated variants for T have already been served
- **THEN** the API serves a recycled variant with reordered choices for free without an Engine call, records its snapshot, emits `retry_variant_generation_failed` and `retry_variant_served`, and does not block zero-balance learners

#### Scenario: Missing or invalid pool fallback
- **WHEN** a retry pool for topic T is missing or invalid
- **THEN** the API serves the source Question with reordered choices, emits `retry_variant_generation_failed`, and requires a correct answer to resolve the topic

#### Scenario: Never auto-pass
- **WHEN** an exhausted pool variant or safe fallback is served
- **THEN** the learner must submit the correct answer to resolve the topic; viewing recaps or encountering errors does not mark the topic passed
