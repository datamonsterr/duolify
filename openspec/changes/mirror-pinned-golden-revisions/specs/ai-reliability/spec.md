## MODIFIED Requirements

### Requirement: Golden set dataset
The Golden set SHALL be a Hugging Face dataset (`datamonsterr/duolify-golden`) where each row has `task`, `input` (JSON), `rubric` and `checks`. The eval binary MUST download only a pinned immutable dataset revision (40-character hex commit SHA). Live evaluations SHALL fail explicitly on non-pinned or unknown revisions. Tasks MUST include `course_preview`, `course_outline`, `lesson_slides`, `lesson_test`, `tutor_chat` and `note_enrich`.

#### Scenario: Pinned revision
- **WHEN** `engine:eval` runs
- **THEN** it uses the revision pinned in `evals/` config and records it and its source in the report

#### Scenario: Unknown or non-pinned revision
- **WHEN** a live evaluation is requested with an unpinned branch name or non-hex revision
- **THEN** the evaluation fails explicitly without attempting download

### Requirement: Eval data backup and retention
The Golden set dataset is versioned on Hugging Face and its revisions MUST be kept. Eval reports MUST be stored as CI artifacts for 90 days. A copy of the pinned dataset revision MUST be mirrored to object storage (RustFS locally or S3). The eval runner MUST verify the SHA-256 checksum of mirrored and cached datasets and reject corrupt or unrelated fallback datasets.

#### Scenario: Hugging Face unavailable with verified mirror
- **WHEN** Hugging Face is unavailable during a live evaluation
- **THEN** the eval downloads and verifies the mirrored copy of the pinned revision and records `dataset_source` as `mirror`

#### Scenario: Hugging Face unavailable with missing or corrupt mirror
- **WHEN** Hugging Face is unavailable and the mirror revision is missing or has a mismatched checksum
- **THEN** the eval fails explicitly and does not silently fall back to mock fixtures

### Requirement: Reliability analytics events
The system SHALL emit `$ai_generation` for every call (with `fallback_used`) and `eval_run_completed` (with `task`, `score`, `threshold`, `passed`, `dataset_revision`, `dataset_source`) for each evaluated task in an eval run, as defined in the `analytics` spec. Operational eval events SHALL use the system UUID `00000000-0000-4000-8000-000000000011` and contain no learner text. An alert MUST fire when the fallback rate or error rate exceeds config limits.

#### Scenario: Task eval completed
- **WHEN** an evaluation finishes for a task
- **THEN** `eval_run_completed` is emitted with task, score, threshold, passed flag, dataset revision, and dataset source
