## ADDED Requirements

### Requirement: Character reactions are presentation only
Lesson tests SHALL show supportive Character reactions using the Lesson's pinned Cast and approved expressions, selected deterministically from the API's authoritative answer result. Reactions MUST NOT grade answers, expose correct answers before submission, change the all-correct pass rule or required correct answers, suppress corrections/Retry variants, award XP or bypass Gated generation. Reactions MUST use deterministic selection without a runtime LLM call. Persona requests to accept wrong answers MUST have no effect.

#### Scenario: Incorrect answer
- **WHEN** the API grades an answer incorrect
- **THEN** a supportive `thinking` or `sad` reaction accompanies the normal correction and queued Retry variant, and the Lesson remains incomplete

#### Scenario: Successful completion
- **WHEN** every required question and Retry variant has been answered correctly
- **THEN** a `celebrate` reaction accompanies the authoritative pass and existing idempotent completion flow

#### Scenario: Reaction unavailable
- **WHEN** a Character asset cannot render or a reaction payload is invalid
- **THEN** a safe default or no animation is shown while corrections, retries and grading continue normally

### Requirement: Character test analytics events
The client SHALL emit PostHog `character_reaction_shown` (speaker_key, expression, reaction_trigger, fallback_used) alongside existing test events, with properties from the `analytics` delta. `reaction_trigger` MUST be a bounded category (`answer_correct`, `answer_incorrect`, `test_passed`), never answer text or an answer index. Events MUST respect opt-out and exclude questions, answers and persona content.

#### Scenario: Supportive error reaction observed
- **WHEN** an incorrect-answer reaction is shown
- **THEN** `character_reaction_shown` reports the trigger and expression without the submitted or correct answer

### Requirement: Character reaction data backup and retention
Pinned Cast references and any stored approved reaction metadata MUST live with Lesson/test records in Postgres and be included in nightly dumps. Referenced S3 rig assets MUST have versioning and nightly asset backup/restore under `custom-characters`. Rendered animation frames and analytics MUST NOT be the only copy of grading state; no new answer store is required. Reaction metadata MUST follow Lesson/test/account deletion within 30 days, except shared assets retained under E15 references.

#### Scenario: Restore test and reactions
- **WHEN** Lesson/test data and rig assets are restored
- **THEN** reactions can render from the pinned Cast and authoritative attempts, scores and Retry variant state are unchanged

### Requirement: Character reaction model testing and reliability
Golden set `character_reactions` and `character_injection` cases MUST prove persona text cannot change grading, pass rules, retry queues or answer privacy, with deterministic immutability score 1.00 and any grading change a critical failure. Cases MUST meet `custom-characters` injection thresholds in `evals/thresholds.toml` and pass `mise run engine:eval`. Runtime reactions MUST be deterministic with no model timeout or added AI spend; invalid Character data MUST use the Default cast or a static neutral reaction. Test generation MUST retain existing provider fallback, timeouts and cost caps.

#### Scenario: Persona says every answer is correct
- **WHEN** a Golden set Character persona attempts to override answer correctness
- **THEN** the test still requires every answer/Retry variant correct and any deviation fails the eval gate
