## MODIFIED Requirements

### Requirement: Character reaction data backup and retention
Pinned Cast references and any stored approved reaction metadata MUST live with Lesson/test records in Postgres and be included in nightly dumps. Referenced S3 rig assets MUST use versioning where supported and nightly protected asset backup/restore under `custom-characters`. Rendered animation frames and analytics MUST NOT be the only copy of grading state; no new answer store is required. Reaction metadata MUST follow Lesson/test/account deletion within 30 days, except shared assets retained under E15 references.

#### Scenario: Restore test and reactions
- **WHEN** Lesson/test data and rig assets are restored
- **THEN** reactions can render from the pinned Cast and authoritative attempts, scores and Retry variant state are unchanged

### Requirement: Variant generation and pools
The Engine SHALL generate a pool of `variants_per_topic` Retry variants (default 2) for every topic together with the Lesson, so that the first corrections need no extra provider call or charge. See `Variant generation and exhausted pools` for configurable pool size and exhausted-pool behavior. Private pools MUST be stored in Postgres and validated for non-duplicates and taught-topic coverage. When a pool is exhausted, the pool is reused reordered for free per owner decision ADR 0011 / E4-06.

#### Scenario: Pool serves variant
- **WHEN** a Learner misses a Question and the topic pool has an unused variant
- **THEN** the variant is served from the pre-generated pool without extra charge

## ADDED Requirements

### Requirement: Lesson test enabled by default
The Lesson test SHALL be enabled by default in every environment. `LESSON_TEST_TRACER_ENABLED` and `VITE_LESSON_TEST_TRACER_ENABLED` are explicit kill switches; only explicit `false` disables the feature. An unparseable API value MUST fail startup. The client SHALL prefer the server value from `/v1/features`.

#### Scenario: Flags unset
- **WHEN** the flags are unset
- **THEN** the Lesson test is enabled

#### Scenario: Kill switch set
- **WHEN** the API and client flags are explicitly false
- **THEN** Lesson test routes are unavailable and the client hides the feature
