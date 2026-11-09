## ADDED Requirements

### Requirement: Character event catalog
The PostHog catalog SHALL include the events and properties below. All events MUST include existing identity/common properties (`distinct_id` as Learner UUID, `platform`, `app_version`, `source`), and `course_id`, `lesson_id`, `slide_id` when known. `operation` MUST be `create`, `regenerate` or `manual_edit`; `cast_source` MUST be `default`, `custom`, `mixed` or `published`. Identifiers/versions MUST be opaque IDs or integers; `fields_changed` MUST contain schema field names only; reason codes MUST be bounded enums rather than provider errors or user content.

| Event | Source | Properties |
|---|---|---|
| `character_create_quoted` | api | operation, credits, pricing_version |
| `character_created` | api | character_id, character_version, rig_version, voice_key, credits, pricing_version |
| `character_regenerated` | api | character_id, character_version, rig_version, voice_key, credits, pricing_version |
| `character_updated` | api | character_id, character_version, fields_changed |
| `character_deleted` | api | character_id |
| `character_rejected` | api | operation, reason_code |
| `character_create_failed` | api | operation, reason_code, fallback_used |
| `course_cast_selected` | api | cast_source, supporting_count, cast_version |
| `lesson_cast_applied` | api | cast_version, cast_source, supporting_count, fallback_used |
| `character_rendered` | client | speaker_key, character_version, rig_version, expression, offline |
| `character_render_fallback` | client | reason_code, offline |
| `character_reaction_shown` | client | speaker_key, expression, reaction_trigger, fallback_used |
| `tutor_character_selected` | client | speaker_key, character_version, cast_source |
| `tutor_character_fallback` | api | reason_code, phase (`before_stream`) |
| `character_voice_fallback` | api | speaker_key, reason_code, voice_config_version |
| `character_snapshot_published` | api | course_version, character_count, custom_count |

#### Scenario: Successful Character creation funnel
- **WHEN** a Learner accepts a Character quote and the job succeeds
- **THEN** one `character_created` event follows the quote event with the actual settled Credits and pricing version

#### Scenario: Manual editing event
- **WHEN** a free field edit succeeds
- **THEN** `character_updated` lists only field names and no creation/spend event is emitted for the edit

E17-03 emits only `character_updated`, `character_deleted`,
`character_rejected(operation=manual_edit)`, `course_cast_selected` and per-attempt
`$ai_generation(feature=character_screen)` from the API. `fields_changed` is an
allowlist of `info.name`, `info.bio`, `info.backstory`, `personality.traits`,
`personality.speaking_style`, `personality.persona_prompt`, `appearance.body_shape`,
`appearance.palette`, `appearance.eyes`, `appearance.mouth`,
`appearance.accessories`, `voice_key`. It never contains field values or dynamic
schema keys. Compare-and-swap version replacement and a successful deletion emit
their outcome once; denied repeats emit no success. Every actual model attempt
records provider/model/token/cache/latency/error/fallback metadata. No model call
is inferred from a deterministic rejection. Existing queued delivery and account
person-deletion apply; learner profile consent suppresses these events.

### Requirement: Character analytics privacy and delivery
Character events and `$ai_generation` MUST exclude raw prompts, names, bios, backstories, persona text, speaking-style text, dialogue, answers, spec JSON/SVG and credentials. Existing opt-out, non-blocking delivery and PostHog person deletion MUST apply. API job events MUST be deduplicated by operation/result ID; render events MUST be once per Slide speaker per visit and publication events once per Course version. Every model attempt MUST report actual usage, provider, model, latency, errors and fallback status with a bounded feature key, including platform-paid screening; prompt/completion capture MUST remain disabled.

#### Scenario: Analytics unavailable
- **WHEN** PostHog is down during creation or grading
- **THEN** the operation continues normally and a background queue handles event delivery without leaking content

#### Scenario: Persona contains personal info
- **WHEN** a Character spec includes learner-provided bio text
- **THEN** neither the bio nor any derived free-text summary appears in product or LLM analytics

### Requirement: Character analytics data backup and retention
PostHog MUST remain a derived store, never the sole source of Character creation, Cast selection or snapshot state. That state MUST live in Postgres with nightly dumps and required versioned S3 assets in nightly protected asset backups per `custom-characters`. Event retention MUST follow the existing analytics policy; account deletion MUST request PostHog person deletion. A raw-content analytics archive MUST NOT be created; product records MUST follow the Character and E15 deletion policies.

#### Scenario: PostHog data lost
- **WHEN** analytics data is unavailable after restore
- **THEN** Character ownership, Casts and published snapshots still resolve entirely from restored product records and assets

### Requirement: Character analytics reliability and tests
Emitters MUST have unit tests and e2e funnel assertions with a PostHog test double, including opt-out, no-content capture and idempotent job delivery. Analytics collection MUST NOT introduce model calls, model timeouts or Credits spend; AI-derived results MUST already pass `custom-characters` Golden set thresholds and bounded fallback before success events are emitted. Invalid or failed mapping MUST emit rejection/failure rather than a paid creation success.

#### Scenario: Failed mapping not counted as success
- **WHEN** all bounded mapping attempts fail and the Default cast is offered
- **THEN** the analytics test observes `character_create_failed` and no `character_created` event or successful spend
