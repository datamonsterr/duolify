## MODIFIED Requirements

### Requirement: Golden set dataset
The Golden set SHALL be a Hugging Face dataset (`datamonsterr/duolify-golden`) where each row has `task`, `input` (JSON), `rubric` and `checks`. The eval binary MUST download only a pinned immutable dataset revision (40-character hex commit SHA) whose expected SHA-256 is committed in `evals/pinned-datasets.toml`, keyed by dataset and revision. Live evaluations SHALL fail explicitly on non-pinned or unknown revisions before cache access or download. Tasks MUST include `course_preview`, `course_outline`, `lesson_slides`, `lesson_test`, `tutor_chat` and `note_enrich`.

#### Scenario: Pinned revision
- **WHEN** `engine:eval` runs
- **THEN** it uses the revision pinned in `evals/` config and records it and its source in the report

#### Scenario: Unknown or non-pinned revision
- **WHEN** a live evaluation is requested with an unpinned branch name or non-hex revision
- **THEN** the evaluation fails explicitly without attempting download

### Requirement: Eval data backup and retention
The Golden set dataset is versioned on Hugging Face and its revisions MUST be kept. Eval reports MUST be stored as CI artifacts for 90 days. A copy of the pinned dataset revision MUST be mirrored to object storage (RustFS locally or S3). The eval runner MUST verify the exact bytes of HF, mirrored and cached datasets against the repository-pinned SHA-256 and reject corrupt or unrelated datasets. Mirror objects MUST include a checksum header or nonempty checksum sidecar; neither remote metadata nor a local sidecar supplies the trusted digest. An existing cache file without a checksum sidecar MUST fail without creating one. Reports MUST record `dataset_verified`, defaulting to false for legacy reports and unverified inputs. The promotion gate MUST refuse unverified datasets regardless of passing scores.

#### Scenario: Hugging Face unavailable with verified mirror
- **WHEN** Hugging Face has a network failure, timeout (including HTTP 408), or HTTP 5xx during a live evaluation
- **THEN** the eval downloads and verifies the mirrored copy of the pinned revision and records `dataset_source` as `mirror`

#### Scenario: Hugging Face unavailable with missing or corrupt mirror
- **WHEN** Hugging Face is unavailable and the mirror revision is missing or has a mismatched checksum
- **THEN** the eval fails explicitly and does not silently fall back to mock fixtures

#### Scenario: Hugging Face client or integrity error
- **WHEN** Hugging Face returns 401, 403, 404, another non-timeout client error, or bytes that mismatch the repository pin
- **THEN** the evaluation fails without requesting the mirror or writing cache files

#### Scenario: Self-signed cache or mirror
- **WHEN** tampered bytes have a matching sidecar or mirror checksum but mismatch the repository pin
- **THEN** the runner rejects the bytes and never records them as verified

#### Scenario: Existing cache has no checksum sidecar
- **WHEN** a cache file exists without its checksum sidecar
- **THEN** the runner fails explicitly and does not generate a sidecar from cached bytes

#### Scenario: Promotion from unverified or legacy report
- **WHEN** an otherwise passing live report has `dataset_verified` false or absent
- **THEN** the promotion gate refuses activation

### Requirement: Reliability analytics events
The system SHALL emit `$ai_generation` for every call (with `fallback_used`) and `eval_run_completed` (with `task`, `score`, `threshold`, `passed`, `dataset_revision`, `dataset_source`) for each evaluated task in an eval run, as defined in the `analytics` spec. Operational eval events SHALL use the system UUID `00000000-0000-4000-8000-000000000011` and contain no learner text. An alert MUST fire when the fallback rate or error rate exceeds config limits.

#### Scenario: Task eval completed
- **WHEN** an evaluation finishes for a task
- **THEN** `eval_run_completed` is emitted with task, score, threshold, passed flag, dataset revision, and dataset source
