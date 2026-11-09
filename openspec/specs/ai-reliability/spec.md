# ai-reliability Specification

## Purpose
Make every AI feature measurable and resilient: a Golden set on Hugging Face, eval thresholds, provider fallback, timeouts and cost limits. Prompt or model changes are tested before merge.

## Requirements

### Requirement: Golden set dataset
The Golden set SHALL be a Hugging Face dataset (`datamonsterr/duolify-golden`) where each row has `task`, `input` (JSON), `rubric` and `checks`. The eval binary MUST download a pinned dataset revision. Tasks MUST include `course_preview`, `course_outline`, `lesson_slides`, `lesson_test`, `tutor_chat` and `note_enrich`.

#### Scenario: Pinned revision
- **WHEN** `engine:eval` runs
- **THEN** it uses the revision pinned in `evals/` config and records it in the report

### Requirement: Feature gate
A feature that uses AI MUST add Golden set cases before merge. A change to prompts, models in `engine/config/models.toml`, or the provider router MUST pass `mise run engine:eval`.

#### Scenario: New AI feature without cases
- **WHEN** a pull request adds an Engine RPC with no Golden set cases
- **THEN** review MUST block the merge

### Requirement: Scoring and thresholds
Each case SHALL be scored by deterministic checks and an LLM judge. Per-task minimum scores MUST be set in `evals/thresholds.toml`. The run MUST fail when any task is below its threshold or when a deterministic check marked `critical` fails.

#### Scenario: Below threshold
- **WHEN** `lesson_test` scores below its threshold
- **THEN** the eval exits non-zero and prints the failing cases

### Requirement: CI modes
Draft pull requests SHALL run evals in mock mode with recorded fixtures at no API cost. Ready pull requests that touch `engine/` MUST run a live sample when `ANTHROPIC_API_KEY` and `HF_TOKEN` secrets exist, and MUST report `eval_run_completed`.

#### Scenario: Draft PR
- **WHEN** a draft pull request runs CI
- **THEN** evals use fixtures and make no network call to providers

### Requirement: Provider fallback
The Engine SHALL use one `LlmProvider` trait with Claude API and OpenRouter implementations plus a mock. The router MUST retry once on retryable errors, then fall back to the OpenRouter route. Model IDs MUST come from `engine/config/models.toml`.

#### Scenario: Primary outage
- **WHEN** the Claude API returns 5xx twice
- **THEN** the router calls the fallback and the response reports the fallback provider

#### Scenario: Both providers fail
- **WHEN** both providers fail
- **THEN** the Engine returns a typed error, the API releases reserved Credits and the client shows a retry option

### Requirement: Timeouts
Each task MUST have a timeout in config. Streaming tasks MUST have a first-token timeout and an idle timeout. Timeouts MUST be below the API request deadline.

#### Scenario: Slow generation
- **WHEN** a call passes its timeout
- **THEN** it is cancelled and counted as a failed attempt

### Requirement: Cost limits
The Engine MUST enforce max input and output tokens per task. The API MUST enforce spend caps from the `credits` spec. A Golden set run MUST have a total budget and stop when it is exceeded.

#### Scenario: Eval budget exceeded
- **WHEN** a live eval run spends more than its budget
- **THEN** the run stops and fails with a budget error

### Requirement: Output validation
The Engine MUST validate every AI output against schema and safety checks before returning it. Invalid output MUST be retried within the attempt limit, then returned as an error. Unsafe Slide HTML MUST never be returned.

#### Scenario: Invalid JSON from model
- **WHEN** the model returns malformed structured output
- **THEN** the Engine retries with a repair prompt and, if still invalid, returns an error

### Requirement: Reliability analytics events
The system SHALL emit `$ai_generation` for every call (with `fallback_used`) and `eval_run_completed` for every eval run, as defined in the `analytics` spec. An alert MUST fire when the fallback rate or error rate exceeds config limits.

#### Scenario: High fallback rate
- **WHEN** the fallback rate exceeds the limit over the window
- **THEN** an alert is raised

### Requirement: Eval data backup and retention
The Golden set dataset is versioned on Hugging Face and its revisions MUST be kept. Eval reports MUST be stored as CI artifacts for 90 days. A copy of the pinned dataset revision MUST be mirrored to object storage. The eval runner MUST verify the SHA-256 checksum of mirrored and cached datasets and reject corrupt or unrelated fallback datasets.

#### Scenario: Hugging Face unavailable
- **WHEN** the dataset cannot be downloaded
- **THEN** the eval uses the mirrored copy of the pinned revision and records the dataset source in the report
