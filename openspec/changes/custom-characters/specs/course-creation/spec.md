## ADDED Requirements

### Requirement: Cast input to Course and Lesson generation
Course preview and Lesson generation SHALL receive an approved Cast snapshot with stable speaker keys, structured Character specs and bounded personas; no selection MUST resolve to the Default cast. The API MUST resolve ownership and current approved versions at job start, persist each Lesson's effective Cast and reuse it on retries and Retry variants. Casts MUST shape narration/dialogue style, not alter the Course outline's learning goals or truth. Existing Gated generation, outline-only generation, idempotency, credit rules and active Course instruction input MUST remain enforced.

#### Scenario: Chosen guide and supporting Character
- **WHEN** a permitted Lesson job starts with one guide and one supporting Character
- **THEN** the Engine receives their approved versioned specs and returns narrator/dialogue segments using only those speaker keys

#### Scenario: Existing Course with no Cast
- **WHEN** a Course created before E17 generates its next unlocked Lesson
- **THEN** the Default cast is used without rewriting previous Lessons or charging Character creation Credits

#### Scenario: Gating remains enforced
- **WHEN** a Learner selects a Cast and requests Lesson N+1 before passing Lesson N
- **THEN** generation is refused and no Engine call occurs

### Requirement: Generation persona boundary and immutable Casts
The Engine MUST serialize Character info/personality as bounded, escaped data below system and Course rules, using the `custom-characters` safety contract. Persona text MUST NOT change schema, safety, grounding, grading, Course instruction scope or pricing. A Character edit, deletion or Cast change MUST NOT mutate already running or generated Lesson content, narration, tests or stored Cast snapshots. A selected Character that cannot safely resolve at a new job start MUST be replaced by an approved Default cast member, and that effective choice MUST be persisted.

#### Scenario: Persona asks to skip tests
- **WHEN** a persona includes a request to remove tests or ignore Course instructions
- **THEN** it cannot override those rules and all normal output validation remains enforced

#### Scenario: Worker retry after edit
- **WHEN** a worker retries a Lesson job after its selected guide was edited
- **THEN** it uses the original job snapshot and settles its original reservation once

### Requirement: Cast generation analytics events
The API SHALL emit PostHog `lesson_cast_applied` (cast_version, cast_source, supporting_count, fallback_used) when persisting the effective Cast, in addition to existing Course events. `$ai_generation` MUST capture actual usage and fallback status without persona or prompt content. Event properties and opt-out MUST follow the `analytics` delta.

#### Scenario: Default used during generation
- **WHEN** an unavailable custom guide is replaced for a Lesson job
- **THEN** `lesson_cast_applied` reports the effective cast version and `fallback_used` true without the Character's info

### Requirement: Generation Cast data backup and retention
Course selections, job/lesson Cast snapshots and schema/rig/voice version references MUST live in Postgres and be included in nightly dumps. Any S3 rig, Slide or narration assets MUST have versioning and nightly protected asset copies/manifests under `custom-characters` and `data-backup`. Course deletion MUST purge unshared snapshots within 30 days; published references MUST follow E15 retention. Raw creation prompts MUST NOT be copied into generation snapshots or backups as Course content.

#### Scenario: Restore a running job
- **WHEN** a backup containing a pending generation job is restored
- **THEN** its saved Cast resolves identically and a retry does not switch to edited library versions or charge twice

### Requirement: Cast generation model testing and reliability
Golden set `character_persona_lesson` and `character_injection` cases MUST cover persona adherence, Cast consistency, no external assets, instruction coexistence, immutable snapshots and unchanged gating/test rules. Deterministic validity/immutability MUST score 1.00; adherence and injection MUST meet `custom-characters` thresholds in `evals/thresholds.toml` and pass `mise run engine:eval`. Persona failures MUST use the Default cast within existing Lesson generation deadlines and token/spend caps, then existing provider fallback; an invalid base Lesson MUST still fail and release its reservation rather than bypass validation.

#### Scenario: Persona unavailable
- **WHEN** a selected spec is invalid or unavailable before output
- **THEN** generation uses the safe default within the existing quote ceiling, records the effective Cast and never saves invalid output
