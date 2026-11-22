# ai-reliability Specification

## Purpose
Make every AI feature measurable and resilient: a Golden set on Hugging Face, eval thresholds, provider fallback, timeouts and cost limits. Prompt or model changes are tested before merge.

## Requirements

### Requirement: Golden set dataset
The Golden set SHALL be a Hugging Face dataset (`datamonsterr/duolify-golden`) where each row has `task`, `input` (JSON), `rubric` and `checks`. The eval binary MUST download only a pinned immutable dataset revision (40-character hex commit SHA) whose expected SHA-256 is committed in `evals/pinned-datasets.toml`, keyed by dataset and revision. Live evaluations SHALL fail explicitly on non-pinned or unknown revisions before cache access or download. Tasks MUST include `course_preview`, `course_outline`, `lesson_slides`, `lesson_test`, `tutor_chat` and `note_enrich`.

#### Scenario: Pinned revision
- **WHEN** `engine:eval` runs
- **THEN** it uses the revision pinned in `evals/` config and records it and its source in the report

#### Scenario: Unknown or non-pinned revision
- **WHEN** a live evaluation is requested with an unpinned branch name or non-hex revision
- **THEN** the evaluation fails explicitly without attempting download

### Requirement: Feature gate
A feature that uses AI MUST add Golden set cases before merge. A change to prompts, models in `engine/config/models.toml`, or the provider router MUST pass `mise run engine:eval`. Canonical tasks (`course_preview`, `course_outline`, `lesson_slides`, `lesson_test`, `tutor_chat`, `note_enrich`) and registered required extension tasks (`variants`, `narration`, `screening`, `steering`) MUST have cases in the dataset; missing cases SHALL fail the feature gate.

#### Scenario: New AI feature without cases
- **WHEN** a pull request adds an Engine RPC with no Golden set cases
- **THEN** review MUST block the merge

#### Scenario: Missing canonical task cases
- **WHEN** the eval runs and any canonical task has zero cases
- **THEN** the eval fails the feature gate

### Requirement: Scoring and thresholds
Each case SHALL be scored by deterministic checks and an LLM judge. Per-task minimum scores MUST be set in `evals/thresholds.toml`. The run MUST fail when any task is below its threshold or when a deterministic check marked `critical` fails. Scores and thresholds MUST be evaluated per canonical task rather than as a global aggregate. Task aliases MUST bridge legacy routes (`preview`, `outline`, `lesson`, `chat`, `note`) to canonical task names (`course_preview`, `course_outline`, `lesson_slides`, `lesson_test`, `tutor_chat`, `note_enrich`). Reports in JSON and Markdown MUST record per-task scores, thresholds, dataset revision, prompt revision, model, and critical check outcomes.

#### Scenario: Below threshold
- **WHEN** `lesson_test` scores below its threshold
- **THEN** the eval exits non-zero and prints the failing cases

#### Scenario: Critical check failure
- **WHEN** a deterministic check marked `critical` fails on any case
- **THEN** the eval run fails immediately regardless of task score or aggregate pass rate

### Requirement: CI modes
Draft pull requests SHALL run evals in mock mode with recorded fixtures at no API cost. Ready pull requests that touch `engine/` MUST run a live sample when `OPENROUTER_API_KEY` and `HF_TOKEN` secrets exist (ADR 0015: OpenRouter is the only provider), and MUST report `eval_run_completed`. In the absence of required secrets, ready runs SHALL report explicit missing-secret status, retain evaluation reports as 90-day artifacts, and keep the `ci-ok` aggregate passing without fabricating a live pass or enabling feature promotion.

#### Scenario: Draft PR
- **WHEN** a draft pull request runs CI
- **THEN** evals use fixtures, make no network call to providers, and run free of API cost

#### Scenario: Ready PR with missing secrets
- **WHEN** a ready pull request runs without live provider credentials
- **THEN** it reports explicit missing-secret status, generates an unpromoted report artifact, keeps `ci-ok` green, and keeps feature activation disabled

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

### Requirement: Timeouts
Each task MUST enforce a task timeout, and streaming tasks MUST enforce both a first-token timeout and an idle timeout. Exceeding any timeout MUST cancel the in-flight provider call, record the attempt as a failed attempt with usage accounting, and not deliver partial unsafe output.

#### Scenario: First token timeout
- **WHEN** the first token is not received within the first-token timeout
- **THEN** the primary attempt is cancelled and may fall back to the secondary route if within limits

#### Scenario: Stream idle timeout
- **WHEN** the interval between consecutive stream tokens exceeds the idle timeout
- **THEN** the stream is terminated with a timeout error and cancelled without post-delta fallback

### Requirement: Cost limits
The Engine MUST enforce max input and output tokens per task. The API MUST enforce spend caps from the `credits` spec. A Golden set run MUST have a total budget and stop when it is exceeded.

#### Scenario: Eval budget exceeded
- **WHEN** a live eval run spends more than its budget
- **THEN** the run stops and fails with a budget error

### Requirement: Output validation
The Engine MUST validate every AI output against schema and safety checks before returning it. Every JSON-returning task MUST request structured output from the provider when the model supports it (`response_format`, routed only to endpoints that honour it) and MUST pass the answer through one shared extraction rule: surrounding whitespace and a single surrounding code fence are removed, nothing else. An answer that is not schema-valid after extraction MUST be rejected; prose, partial JSON and unknown enum values MUST NOT be interpreted. A rejected answer MUST be followed, within the attempt limit, by one corrective repair that quotes the validation error, and if still invalid the Engine returns an error. Safety screens MUST stay fail closed. Unsafe Slide HTML MUST never be returned.

#### Scenario: Invalid JSON from model
- **WHEN** the model returns malformed structured output
- **THEN** the Engine retries once with a repair turn quoting the validation error and, if still invalid, returns an error

#### Scenario: Fenced JSON
- **WHEN** the model wraps a valid JSON answer in one ```` ```json ```` fence
- **THEN** the Engine strips the fence, validates the JSON and delivers the unfenced JSON

#### Scenario: Prose safety verdict
- **WHEN** a screen answers `**verdict:** pass` or `allowed: true`
- **THEN** the answer is invalid, the repair attempt runs, and if no schema-valid verdict arrives the screen fails closed

#### Scenario: Unknown reason code
- **WHEN** a screen answers a code outside its closed set, such as `secrets`
- **THEN** the answer is invalid and the repair turn names the allowed codes; the code is never mapped silently

### Requirement: Reliability analytics events
The system SHALL emit `$ai_generation` for every call (with `fallback_used`) and `eval_run_completed` for every eval run, as defined in the `analytics` spec. Derived dashboards SHALL publish task quality, fallback rate, error rate, latency, token usage, cache performance, provider cost, and paid margin dimensions without prompts or learner text. An alert MUST fire when the fallback rate or error rate exceeds configurable limits. Notifications MUST be deduplicated while preserving independent server operational counters. Operational enforcement and server counters MUST continue when analytics is unavailable or a learner opts out.

#### Scenario: High fallback rate
- **WHEN** the fallback rate exceeds the limit over the window
- **THEN** a deduplicated alert is raised and independent server counters record the incident

#### Scenario: Reliability dashboard query
- **WHEN** reliability dashboard metrics are retrieved
- **THEN** task quality, fallback, error, and latency dimensions are returned with zero prompts or learner content

### Requirement: Eval data backup and retention
The Golden set dataset is versioned on Hugging Face and its revisions MUST be kept. Eval reports MUST be stored as CI artifacts and in Postgres for 90 days. Operational aggregates and alert configuration MUST be stored in Postgres and included in database dumps.

#### Scenario: Report retention
- **WHEN** an evaluation run completes
- **THEN** its structured report is retained for 90 days

### Requirement: Eval HTTP access control
Every read under `/v1/eval/` SHALL require a live session via `Accounts.Protect`, a `pricing_admin_grants` grant, and authentication within the last 15 minutes, using the shared pricing admin policy. Report ingest (`POST /v1/eval/reports`) SHALL accept only a service bearer token configured through `EVAL_REPORT_SERVICE_TOKEN`, compared in constant time, and SHALL reject all ingest when that setting is empty. An admin session SHALL NOT authorize ingest. Request bodies SHALL be bounded by `http.MaxBytesReader` to 1 MiB and oversized reports SHALL return HTTP 413 before persistence. Internal errors SHALL be logged server-side and every HTTP 500 SHALL return only a generic error.

This access-control fix SHALL preserve existing Postgres backup and 90-day report retention, operational counters, and PostHog event schemas. It SHALL introduce no new analytics events, prompts, model calls, or model routing changes; existing model eval and fallback requirements continue to apply.

#### Scenario: Anonymous or non-admin dashboard access
- **WHEN** an anonymous or ordinary authenticated learner requests any dashboard, alert list, or report list
- **THEN** the API returns HTTP 401 for anonymous access or HTTP 403 for a non-admin, without exposing operational data

#### Scenario: Admin dashboard access
- **WHEN** an admin with a live session and authentication within the last 15 minutes requests an eval dashboard, alert list, or report list
- **THEN** the API returns HTTP 200 with the requested operational data

#### Scenario: Stale admin authentication
- **WHEN** an admin's last authentication is older than 15 minutes
- **THEN** eval reads return HTTP 403 until the admin reauthenticates

#### Scenario: Forged report ingest
- **WHEN** a report is submitted without the configured service bearer token, with an admin session instead, or while the service token is unset
- **THEN** the API returns HTTP 401 and persists no report

#### Scenario: Authorized service report ingest
- **WHEN** a trusted service submits valid report JSON with the configured service bearer token
- **THEN** the API returns HTTP 201 and persists the report with existing retention

#### Scenario: Oversized report ingest
- **WHEN** an authorized service submits a body exceeding 1 MiB, including a streamed body or trailing whitespace
- **THEN** the API returns HTTP 413 and persists no report

#### Scenario: Internal error redaction
- **WHEN** an authorized eval request encounters an internal failure
- **THEN** the API logs the underlying error and returns HTTP 500 with `internal server error`, without raw database errors or connection details

### Requirement: Live promotion gate
Promotion of new AI models or voices to production SHALL require passing all relevant task thresholds on a pinned Golden set dataset revision during a live evaluation run. Feature activation MUST remain disabled if live evaluation was skipped due to missing secrets or run in mock mode. Evaluation runs MUST immediately reject critical safety, ledger, and cost check failures.

#### Scenario: Feature activation without live pass
- **WHEN** a model change is submitted without passing live evaluation on pinned dataset cases
- **THEN** feature promotion is rejected and production activation remains disabled

#### Scenario: Critical check failure during evaluation
- **WHEN** an evaluation run encounters a critical safety, ledger, or cost check failure
- **THEN** the run fails immediately even if overall score exceeds the task threshold

#### Scenario: Deployed engine with passing live evidence
- **WHEN** a live-mode engine is built with a committed live report that passes the promotion gate on a pinned, digest-verified revision and matches the build fingerprint (route config, thresholds, prompt and provider sources)
- **THEN** each RPC whose Golden set task passed in that report calls the live route, and safety screens stay fail closed on invalid answers

#### Scenario: Deployed engine without valid evidence
- **WHEN** a live-mode engine has no committed live report, a rejected report, a report of another build, or no passing case for the RPC's task
- **THEN** the RPC makes no provider call and fails with UNAVAILABLE and a typed `google.rpc.ErrorInfo` reason (`promotion_evidence_missing`, `promotion_evidence_rejected`, `promotion_evidence_stale`, `promotion_task_not_passed`), logged as `live_feature_not_promoted`; the API keeps its retryable "unavailable" response

#### Scenario: Live voice without evidence
- **WHEN** a live narration vendor is configured and no `lesson_narration` live case passed
- **THEN** synthesis fails closed with the same typed reason and the API falls back to captions

### Requirement: Partial streaming usage and tutor failure cases
A chat stream that ends or fails before its final usage frame MUST still yield one usage record: provider-reported when present, otherwise estimated from delivered bytes, bounded by the quote limits and flagged `estimated`. Golden-set `tutor_chat` cases SHALL cover first-token timeout with fallback, first-token failure with no fallback, idle timeout, cancel and mid-stream failure with critical checks (no second provider after a delta, usage within the quote, typed error code).

#### Scenario: Stream ends without usage frame
- **WHEN** the provider closes after deltas without a usage frame
- **THEN** the Engine emits one estimated usage frame no larger than the quote bounds

### Requirement: Long-course tutor cases
The `tutor_chat` Golden set MUST include long-course retrieval, grounding, refusal, injection-in-retrieved-lesson and cold-cache cases with critical checks, run by `mise run engine:eval` from recorded mock fixtures.

#### Scenario: Injection in retrieved lesson
- **WHEN** an excerpt contains instructions or forged delimiters
- **THEN** the prompt keeps exactly one delimited block, instructions stay clean and the recorded reply refuses and stays on the Course

### Requirement: No new AI behaviour
E8-04 SHALL add no model call, prompt text or fallback. Existing tutor evaluation gates and fallbacks apply unchanged.

#### Scenario: Gates unchanged
- **WHEN** the tutor evaluation runs
- **THEN** its cases and thresholds are the E8-03 ones

### Requirement: Deleted history excluded from tutor context
The `tutor_chat` evaluation SHALL include regression cases where a deleted thread's content must not appear in later answers or context, using recorded mock fixtures and gating at the existing tutor thresholds. E8-05 adds no model call, prompt or fallback.

#### Scenario: Deleted fact
- **WHEN** a later turn follows a deletion and the model would only know the fact from the deleted thread
- **THEN** the reply does not assert it

### Requirement: Course screening reliability
`screen_course` SHALL have schema-checked output (`verdict`, closed `reason_codes`), a 120 second deadline, one evaluated fallback within the USD cap, and golden-set rows for safe, borderline, multilingual, deep-hidden-payload and injection cases at a pinned `datamonsterr/duolify-golden` revision with recorded mock fixtures. `mise run engine:eval` MUST pass. Live promotion stays gated by E10-06 (ADR 0011).

#### Scenario: Malformed verdict
- **WHEN** the provider returns an invalid verdict twice
- **THEN** the engine reports failure and the API stores `pending_screening`

### Requirement: Narration vendor golden rows
`lesson_narration` SHALL include multilingual, timing, vendor-failure and cost-cap golden rows backed by the recorded fixture `evals/fixtures/narration.vendor.json`, replayed against a local fake server. Live promotion stays disabled until E10-06.

#### Scenario: Cost cap
- **WHEN** a request asks for five attempts and a 120 s timeout
- **THEN** the Engine clamps to two attempts and 60 s and charges only delivered characters

### Requirement: Published-copy missing Lesson reliability
The Golden set SHALL contain recorded offline cases for a missing Lesson in a published copy: success, schema failure with bounded fallback, gate refusal (previous not passed) and insufficient credits. Each MUST enforce schema checks, the deadline, attempts and USD caps from the quote, and refuse before any provider call when a gate fails.

#### Scenario: Gate refusal makes no provider call
- **WHEN** the previous Lesson is not passed or credits are insufficient
- **THEN** the check fails and the recorded provider is never consumed

### Requirement: Quote route coverage is visible
The API and worker SHALL log an error at startup for every quoted action whose active pricing
route the Engine will refuse, naming the action, the pricing version, the quoted route and the
Engine route. CI MUST fail when the Engine's task routes in `engine/config/models.toml` are not
covered by the newest routing quote policy proposal. Neither check changes quoting or serving.

#### Scenario: Active pricing quotes a retired route
- **WHEN** the active pricing version quotes a provider the Engine does not serve
- **THEN** startup logs an error per affected action instead of the first learner seeing a 502

#### Scenario: Engine route changes without a matching pricing proposal
- **WHEN** `engine/config/models.toml` routes a quoted task to a model the newest routing proposal does not quote
- **THEN** the API test suite fails and names the action and both routes

### Requirement: Live golden fidelity
Live golden cases for non-screen tasks MUST run the production prompt builders, validators and bounded runner on production request data, not dataset text sent verbatim. Screen cases MUST run the production screen functions.

#### Scenario: Live lesson case
- **WHEN** the live eval runs a lesson case
- **THEN** it builds the production lesson prompt from the case's outline, lesson and learner data and scores whether the production validator delivered the lesson
