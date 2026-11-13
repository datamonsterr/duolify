## ADDED Requirements

### Requirement: Published Character snapshot allowlist
Publishing or republishing SHALL snapshot the approved Cast and Characters attached to the Course, including each generated Lesson's effective Cast, with immutable Character specs, schema/rig/voice-config versions and required rig/narration assets. The publish flow MUST disclose that Character name, bio, backstory, speaking style, persona and appearance become Course content and MUST require confirmation before export. E15's snapshot allowlist MUST include only those content fields, not raw creation prompts, owner identifiers, private library links, screening records or unrelated Characters. Exported fields and appearance MUST pass screening; failure/unavailable screening MUST block publication of the new version without affecting private learning.

#### Scenario: Publish with a custom guide
- **WHEN** the owner confirms the Character disclosure and the new version passes screening
- **THEN** the frozen Course version includes its guide's approved spec/assets and the existing E15 content hash covers them

#### Scenario: Raw prompt in export
- **WHEN** a snapshot candidate contains a Character creation prompt or a private library owner ID
- **THEN** the allowlist scanner rejects publication and no private field becomes visible

#### Scenario: Unsafe Character during publish
- **WHEN** attached Character data fails current screening or the screener is unavailable
- **THEN** the new version stays unpublished and existing approved Course versions remain unchanged

### Requirement: Pinned Character reuse and Course copy boundaries
A Course copy MUST reuse its published version's Cast and Character assets at zero `character_create` cost, including as input to on-demand generation beyond published Lessons. Such new Lessons MUST retain E15's Lesson/narration quotes and charges. Library edits/deletion MUST NOT change a published Character snapshot; republishing MUST create a new Course version and existing copies MUST remain pinned under E15 update rules. An unforked Course copy MUST refuse Cast overrides and explain the published Cast/steering-fork choices. After the existing confirmed Course steering fork, the API SHALL permit the Learner to select an owned Cast for future jobs, but prior Lessons MUST remain unchanged.

#### Scenario: Owner changes the Character
- **WHEN** the owner changes a guide's appearance after publishing version 1
- **THEN** copies pinned to version 1 keep the old appearance and only a republish can expose a newer Character version

#### Scenario: Beyond published Lessons
- **WHEN** a Learner funds the next on-demand Lesson beyond the published snapshot
- **THEN** the job uses the pinned published Cast and charges Lesson/narration prices without Character creation Credits

#### Scenario: Override refused before fork
- **WHEN** a Learner tries to replace the Cast of an unforked Course copy
- **THEN** no selection changes and the client explains that the published Cast remains until the existing confirmed steering fork

### Requirement: Shared Character deletion and safety takedown
Deleting or unpublishing the source Character/Course MUST stop future private selection/new enrollment as appropriate but MUST preserve approved snapshot content while enrolled Course copies reference it, following E15. Creator account deletion MUST remove personal ownership data and pseudonymize attribution without exposing library records. Unsafe Character takedowns MUST suppress the affected appearance/persona in existing copies and use an approved Default cast fallback without changing grading; immutable stored payloads MUST NOT be silently rewritten. Unreferenced published payloads/assets MUST purge 30 days after the last reference ends.

#### Scenario: Creator account deleted
- **WHEN** the creator deletes their account while other Learners retain Course copies
- **THEN** private Character records and personal ownership data follow account deletion while the approved content-only published payload remains with pseudonymous attribution

#### Scenario: Character safety takedown
- **WHEN** a published Character is marked unsafe
- **THEN** runtime rendering/persona resolution suppresses it in favor of a safe default and existing test results stay unchanged

### Requirement: Shared Character analytics events
The API SHALL emit PostHog `character_snapshot_published` (course_version, character_count, custom_count) once for each successfully published version containing a Cast. Published reuse MUST retain existing E15 events; Character default substitution MUST use the relevant rendering/tutor/voice fallback event from the `analytics` delta. Events MUST respect opt-out and exclude Character content, raw prompts and private ownership data.

#### Scenario: Snapshot event
- **WHEN** publication with a Cast succeeds
- **THEN** `character_snapshot_published` reports the Course version and aggregate Character counts without names or personas

### Requirement: Shared Character data backup and retention
Published Cast/spec/rig/voice payloads, hashes, moderation suppression state and Course copy references MUST live in Postgres and nightly dumps. S3 rig/compiled SVG/narration assets MUST be versioned and included in nightly protected copies and asset manifests; restore MUST resolve each copy's pinned versions under `custom-characters` and `data-backup`. Payloads/assets MUST follow E15 last-reference plus 30-day retention; raw prompts and private screening data MUST never enter published backups as Course content. Deletion and takedown records MUST be reapplied before restored versions are served.

#### Scenario: Restore published Characters
- **WHEN** database and asset backups are restored after the creator's private library is deleted
- **THEN** enrolled copies resolve their original approved Character snapshots without needing that library or a paid model call

### Requirement: Shared Character model testing and reliability
Golden set `character_snapshot`, `character_screen` and `character_injection` cases MUST cover content-only export, privacy, immutable reuse, age/IP/likeness screening and persona injection. Snapshot/privacy validity MUST score 1.00; screening and injection MUST meet `custom-characters` thresholds in `evals/thresholds.toml` and pass `mise run engine:eval`. Character screening MUST use the 10 s per-call deadline and bounded retry/provider fallback within E15's publish deadline and platform-paid screening cost cap. Unavailable screening MUST fail closed; runtime invalid Characters MUST use the Default cast within existing generation/chat budgets, and base generation failure MUST release its reservation.

#### Scenario: Private field regression
- **WHEN** an export/model change includes a raw creation prompt or alters a pinned Character payload
- **THEN** deterministic snapshot checks fail and publication cannot pass the eval gate
