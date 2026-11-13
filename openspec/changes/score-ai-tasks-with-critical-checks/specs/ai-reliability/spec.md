## MODIFIED Requirements

### Requirement: Scoring and thresholds
Each case SHALL be scored by deterministic checks and an LLM judge. Per-task minimum scores MUST be set in `evals/thresholds.toml`. The run MUST fail when any task is below its threshold or when a deterministic check marked `critical` fails. Scores and thresholds MUST be evaluated per canonical task rather than as a global aggregate. Task aliases MUST bridge legacy routes (`preview`, `outline`, `lesson`, `chat`, `note`) to canonical task names (`course_preview`, `course_outline`, `lesson_slides`, `lesson_test`, `tutor_chat`, `note_enrich`). Reports in JSON and Markdown MUST record per-task scores, thresholds, dataset revision, prompt revision, model, and critical check outcomes.

#### Scenario: Below threshold
- **WHEN** `lesson_test` scores below its threshold
- **THEN** the eval exits non-zero and prints the failing cases

#### Scenario: Critical check failure
- **WHEN** a deterministic check marked `critical` fails on any case
- **THEN** the eval run fails immediately regardless of task score or aggregate pass rate

### Requirement: Feature gate
A feature that uses AI MUST add Golden set cases before merge. A change to prompts, models in `engine/config/models.toml`, or the provider router MUST pass `mise run engine:eval`. Canonical tasks (`course_preview`, `course_outline`, `lesson_slides`, `lesson_test`, `tutor_chat`, `note_enrich`) and registered required extension tasks (`variants`, `narration`, `screening`, `steering`) MUST have cases in the dataset; missing cases SHALL fail the feature gate.

#### Scenario: New AI feature without cases
- **WHEN** a pull request adds an Engine RPC with no Golden set cases
- **THEN** review MUST block the merge

#### Scenario: Missing canonical task cases
- **WHEN** the eval runs and any canonical task has zero cases
- **THEN** the eval fails the feature gate

### Requirement: Reliability analytics events
The system SHALL emit `$ai_generation` for every call (with `fallback_used`) and `eval_run_completed` (with `task`, `score`, `threshold`, `passed`) for each evaluated task in an eval run, as defined in the `analytics` spec. Operational eval events SHALL use the system UUID `00000000-0000-4000-8000-000000000011` and contain no learner text. An alert MUST fire when the fallback rate or error rate exceeds config limits.

#### Scenario: High fallback rate
- **WHEN** the fallback rate exceeds the limit over the window
- **THEN** an alert is raised

#### Scenario: Task eval completed
- **WHEN** an evaluation finishes for a task
- **THEN** `eval_run_completed` is emitted with task, score, threshold, and passed flag

### Requirement: Eval data backup and retention
The Golden set dataset is versioned on Hugging Face and its revisions MUST be kept. Eval reports MUST be stored as CI artifacts for 90 days. A copy of the pinned dataset revision SHOULD be mirrored to object storage.

#### Scenario: Hugging Face unavailable
- **WHEN** the dataset cannot be downloaded
- **THEN** the eval uses the mirrored copy of the pinned revision
