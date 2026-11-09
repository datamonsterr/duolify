## ADDED Requirements

### Requirement: Character rendering in Motion HTML
The player SHALL render Characters from the Lesson's pinned Cast and validated Character specs using only trusted, versioned original SVG rig parts. Slides MUST support a guide narrator and supporting dialogue speakers identified by stable Cast speaker keys, with `idle`, `happy`, `sad`, `thinking`, `celebrate` expressions. Rendering MUST retain the existing iframe sandbox and CSP, MUST NOT permit arbitrary SVG/HTML/URLs from Character data, and MUST keep readable speaker labels and dialogue/captions when audio or animation is unavailable.

#### Scenario: Two speakers
- **WHEN** a Slide has guide narration followed by a supporting Character's dialogue
- **THEN** the correct pinned Character and speaker label are displayed for each segment without changing their base appearance

#### Scenario: Untrusted appearance markup
- **WHEN** Character data contains an event handler, script or external asset URL
- **THEN** validation rejects it and the player uses a safe built-in appearance without relaxing the sandbox

### Requirement: Character playback and offline fallback
Character expression/animation transitions MUST follow the Slide playback clock through pause, seek, replay and speed changes. The player MUST preload/cache pinned rig/spec assets with the Lesson and support offline replay. Missing, invalid or unsupported assets MUST use the bundled Default cast; text, controls and captions MUST remain available. Reduced-motion mode MUST use static expressions without hiding dialogue.

#### Scenario: Seek and replay
- **WHEN** the Learner pauses, seeks back and replays a dialogue Slide
- **THEN** the Character states and captions match the restored Slide time and speaker order

#### Scenario: Missing rig offline
- **WHEN** a cached Lesson cannot resolve a custom rig while offline
- **THEN** a bundled default renders and the Lesson remains playable without network or model calls

### Requirement: Character player analytics events
The client SHALL emit PostHog `character_rendered` (speaker_key, character_version, rig_version, expression, offline) once per Slide speaker and `character_render_fallback` (reason_code, offline) when a slot falls back, with common Course/Lesson/Slide properties from the `analytics` delta. Events MUST respect opt-out and exclude Character text, SVG and dialogue.

#### Scenario: Render fallback event
- **WHEN** a missing rig causes a bundled fallback
- **THEN** `character_render_fallback` reports the bounded reason and offline state without asset contents

### Requirement: Character player backup and retention
Pinned Cast/spec references and speaker/expression metadata MUST be stored with Lesson manifests in Postgres and covered by nightly dumps. Versioned S3 rig/compiled assets MUST be covered by nightly protected asset copies/manifests and restore checks under `custom-characters` and `data-backup`. Device caches MUST be disposable. Data MUST follow Lesson deletion within 30 days and E15 reference retention for shared assets; shared objects MUST NOT be purged while referenced.

#### Scenario: Restore playable appearance
- **WHEN** the Lesson database and asset backup are restored
- **THEN** every manifest resolves its pinned Character assets without model calls and the player can cache it again

### Requirement: Character Slide model testing and reliability
Golden set `character_persona_lesson` cases MUST check valid speaker keys, supported expressions, schema/rig validity, safe HTML and persona adherence; scores MUST meet `custom-characters` thresholds in `evals/thresholds.toml` and pass `mise run engine:eval`. Rendering MUST be deterministic with no runtime LLM cost. Character generation failure MUST use the Default cast within the existing Lesson generation timeout and spend cap; if base Slide generation remains invalid, the existing retry/fallback and reservation release rules MUST apply.

#### Scenario: Unknown generated speaker
- **WHEN** a generated dialogue references a speaker outside the pinned Cast
- **THEN** validation rejects it, bounded retry/default regeneration occurs and no invalid reference reaches the player
