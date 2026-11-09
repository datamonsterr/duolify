## ADDED Requirements

### Requirement: Character Tutor chat persona
Tutor chat SHALL use the main guide as its default persona and allow the Learner to choose a supporting Character from the effective Cast. Inside a Lesson the default Cast MUST be its pinned Lesson snapshot; outside it MUST be the Course's current approved Cast, or the Default cast when none is selected. The API MUST pin the Character version for each turn. The Engine MUST receive validated persona/info only as a bounded escaped data section below system policy, together with the existing Course, Lesson, profile and history context. Persona style MUST NOT override grounding, safety, tools, output rules, grading or Credits.

#### Scenario: Guide answers in character
- **WHEN** a Learner asks a Course question without choosing a different tutor
- **THEN** the reply uses the effective main guide's speaking style while staying grounded in the Course

#### Scenario: Persona changes between turns
- **WHEN** a Learner changes the tutor after a reply has started
- **THEN** the active turn keeps its pinned Character and the next turn uses the new choice without rewriting messages

#### Scenario: Injection in persona
- **WHEN** a selected persona asks to reveal system instructions or bypass credit checks
- **THEN** the tutor does neither and the normal safety/credit rules remain enforced

### Requirement: Tutor persona analytics events
The system SHALL emit PostHog `tutor_character_selected` (speaker_key, character_version, cast_source) and `tutor_character_fallback` (reason_code, phase), alongside existing tutor events and `$ai_generation` usage. Events MUST follow the `analytics` delta, respect opt-out and contain no Character info, persona, message or prompt text.

#### Scenario: Persona fallback recorded
- **WHEN** an invalid persona is replaced before a reply
- **THEN** `tutor_character_fallback` has `phase` `before_stream` and a bounded reason code

### Requirement: Tutor Character data backup and retention
Each chat turn's effective Character/version and immutable spec reference MUST be stored with the thread in Postgres and included in nightly dumps. Referenced S3 rig assets MUST follow versioned nightly asset backup/restore under `custom-characters`; no new tutor audio asset is required in v1. Thread deletion MUST remove turn references and unshared retained persona snapshots, and account deletion MUST remove them within 30 days. Shared Lesson/published references MUST retain only their independently required content under their existing policies.

#### Scenario: Thread deleted
- **WHEN** a Learner deletes a thread
- **THEN** its messages and per-turn Character references leave later context and cannot keep an otherwise unreferenced private spec alive

### Requirement: Tutor persona model testing and reliability
Golden set `character_persona_chat` and `character_injection` cases MUST cover adherence, grounding, refusals, hidden-prompt protection and version consistency; they MUST meet `custom-characters` thresholds in `evals/thresholds.toml` and pass `mise run engine:eval`. Invalid/unavailable persona context MUST fall back to an approved Default cast persona before streaming, within existing first-token/idle timeouts and turn token/spend caps. Model failures MUST use existing provider fallback before the first delta; mid-stream failures MUST end once with an error and release unused Credits, without restarting in another persona. Streaming outputs MUST retain the existing safety checks.

#### Scenario: Safe default before streaming
- **WHEN** a custom tutor spec fails validation at turn start
- **THEN** one safe default reply is attempted within the existing turn ceiling and no malformed persona reaches the Engine

#### Scenario: Mid-stream failure
- **WHEN** a provider fails after sending deltas in a Character's voice
- **THEN** the reply ends with the existing retry option and no second persona reply is spliced into it
