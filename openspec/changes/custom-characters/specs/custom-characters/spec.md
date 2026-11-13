## ADDED Requirements

### Requirement: Prompt-based Character creation and regeneration
A Learner SHALL be able to create an original Character from a prompt specifying personality, appearance and info (name, bio, backstory and speaking style), and regenerate an owned Character from a new prompt. The Engine MUST return a structured `CharacterSpec` that passes the versioned, closed JSON Schema and cross-field constraints in the design before persistence. The API MUST enforce ownership and idempotent background jobs; regeneration MUST preserve the previous approved version until successful replacement.

#### Scenario: Create from a prompt
- **WHEN** a Learner submits an allowed prompt and confirms a funded quote
- **THEN** one validated Character version is saved privately and the Learner can preview its appearance and info

#### Scenario: Failed regeneration
- **WHEN** regeneration returns malformed or unsafe output after bounded attempts
- **THEN** no new version becomes current and the previous approved Character is unchanged

#### Scenario: Duplicate request
- **WHEN** the same Learner retries a creation request with the same idempotency key
- **THEN** the same job/result is returned and no duplicate Character or charge is created

### Requirement: Original parametric Character rig
Appearance SHALL use Duolify's original parametric SVG Character rig with body shape, palette, eyes, mouth, accessories and expressions `idle`, `happy`, `sad`, `thinking`, `celebrate`. The validated spec MUST contain only bounded parameters and trusted asset/voice keys; it MUST NOT contain executable SVG/HTML, scripts, external URLs or provider IDs. V1 MUST NOT invoke free-form image generation; optional AI portraits are outside this change. Schema and rig versions MUST remain resolvable for stored Lessons.

#### Scenario: Consistent expressions
- **WHEN** the same Character appears idle on one Slide and celebrating on another
- **THEN** its base body, palette and accessories remain identical and only the trusted expression/animation changes

#### Scenario: Unsupported appearance
- **WHEN** a model adds an unknown property or an SVG string to the spec
- **THEN** schema validation rejects it and no such appearance is rendered or saved

### Requirement: Structured personality and free manual editing
A Character SHALL have structured info and personality fields plus a short persona prompt, within the design's limits. A Learner MUST be able to list, inspect, edit and delete owned Characters. Manual field edits MUST be free of Credits, MUST NOT invoke paid prompt-to-spec mapping, and MUST pass the same schema and safety screening before creating an immutable version. Voice edits MUST select a key from the approved ElevenLabs allowlist. Existing Lesson, chat and published content MUST remain unchanged after edits.

#### Scenario: Manual edit
- **WHEN** the Learner changes a Character's bio, speaking style and palette to valid allowed values
- **THEN** a new approved version is saved with zero Credits spent and past Lessons remain unchanged

#### Scenario: Another Learner's Character
- **WHEN** a Learner attempts to read, edit or select a private Character belonging to someone else
- **THEN** the API denies access without disclosing its contents

### Requirement: Per-Course Cast and Default cast
A Learner SHALL choose one main guide and zero to two distinct supporting Characters per owned Course or forked Course copy. The main guide MUST be the default Tutor chat persona; Characters MUST be usable as Slide narrators, dialogue speakers and Lesson test reactions. With no selection, the system MUST use a few original built-in Characters as the Default cast, at no Character creation charge. Built-in names, art and designs MUST NOT copy or reference Duolingo's or any other game's characters. Every generation job MUST pin approved versions and effective fallback slots at start; retries MUST reuse them and later edits MUST affect only future jobs.

#### Scenario: No Cast chosen
- **WHEN** Lesson generation starts without a saved Cast
- **THEN** the configured Default cast is pinned and used with no `character_create` spend

#### Scenario: Edit during generation
- **WHEN** a Learner edits a selected Character or changes the Cast after a Lesson job has started
- **THEN** that job keeps its original snapshot and the change applies to the next new job

### Requirement: Character safety and bounded persona context
Prompts, generated specs including compiled appearance, and manual edits MUST be screened for real-person likeness/impersonation, third-party IP/trademarked characters, sexual content, age-inappropriate content and prompt injection. Rejections MUST show bounded reason codes; unavailable screening MUST fail closed for writes. The Engine MUST pass only bounded, validated persona/info text as escaped data below system policy, following Course steering, and MUST prevent it from changing safety, tools, output format, grounding, grading, pricing or revealing hidden prompts. Generated lesson/chat outputs MUST pass their normal validation and safety gates.

#### Scenario: Restricted character request
- **WHEN** a prompt asks for a real-person likeness, a trademarked character or sexual content
- **THEN** it is rejected before a Character is saved and the Learner receives a policy reason code

#### Scenario: Malicious backstory
- **WHEN** a backstory attempts to close the data delimiter and instruct the tutor to reveal prompts or mark wrong answers correct
- **THEN** it is rejected or safely treated as data, no hidden prompt is disclosed and grading remains unchanged

#### Scenario: Screening unavailable
- **WHEN** input or output screening times out
- **THEN** no new version is saved, an existing version stays usable and any creation reservation is released

### Requirement: Per-Learner limits
The API MUST atomically enforce configurable limits, initially proposed as 10 active custom Characters per Learner, 5 create/regenerate attempts per day and 2 per minute, 20 manual edits per hour, prompts of 1–1,500 characters, spec size at most 8 KiB and combined persona/info text at most 2,400 characters. Built-ins MUST NOT consume the ownership quota. Rejected requests MUST consume applicable attempt quotas but MUST NOT spend Credits.

#### Scenario: Count limit
- **WHEN** a Learner with 10 active custom Characters requests another
- **THEN** the request is refused with `limit_exceeded`, no mapping call occurs and no Credits are spent

### Requirement: Character creation Credits
Creating or prompt-regenerating a Character MUST quote, reserve and settle Credits at Pricing config key `character_create`, with the immutable pricing version and quote ceiling recorded on ledger operations. This new key and price MUST receive owner approval in a new Pricing config version before creation/regeneration is enabled; no price is assumed here. Manual edits, Cast selection and Default cast use MUST be free of Character creation charges. Retries MUST share one reservation. Failed, rejected or cancelled creation without an accepted result MUST release its reservation; default fallback MUST NOT count as paid success.

#### Scenario: Key not approved
- **WHEN** no owner-approved active version contains `character_create`
- **THEN** paid creation/regeneration is disabled while built-in Cast selection remains available

#### Scenario: Insufficient Credits
- **WHEN** the Learner cannot cover the quote
- **THEN** the API returns `insufficient_credits` and does not invoke prompt-to-spec mapping

#### Scenario: Successful creation
- **WHEN** a valid screened spec is saved
- **THEN** the reservation settles once at its quoted pricing version and never exceeds the quote

### Requirement: Character privacy and publication boundary
Custom Characters MUST be private except for explicit, disclosed inclusion in a Published course snapshot under `course-sharing`. Publication MUST copy only attached approved Cast/spec/rig/voice data and necessary immutable assets, never raw creation prompts, private ownership/library references, screening records or unrelated Characters. Snapshot edits and deletion MUST follow E15 version/reference semantics; readers MUST NOT gain access to the owner's private Character library.

#### Scenario: Private Character attached to a published Course
- **WHEN** the owner confirms the Character export disclosure and publishes
- **THEN** enrolled Learners can use the frozen Character content with that Course but cannot read its creation prompt or private library record

### Requirement: Character analytics events
The system SHALL emit PostHog `character_create_quoted` (operation, credits, pricing_version), `character_created` and `character_regenerated` (character_id, character_version, rig_version, voice_key, credits, pricing_version), `character_updated` (character_id, character_version, fields_changed), `character_deleted` (character_id), `character_rejected` (operation, reason_code), `character_create_failed` (operation, reason_code, fallback_used) and `course_cast_selected` (cast_source, supporting_count, cast_version), as cataloged in the `analytics` delta. `$ai_generation` MUST report each model attempt's actual usage. Common properties, opt-out and no-content capture rules MUST apply.

#### Scenario: Creation observed privately
- **WHEN** creation succeeds
- **THEN** `character_created` carries identifiers and pricing metadata and no prompt, name, bio, persona or appearance content

### Requirement: Character data backup and retention
Character ownership, immutable versions, screening verdicts, Course Casts and Lesson/chat/published Character snapshots or references MUST live in Postgres and be included in encrypted nightly dumps with 14-day retention. Any S3 rig/compiled SVG assets and references MUST have versioning and nightly protected asset copies/manifests, restored together with the database. Unreferenced private records/assets MUST be purged within 30 days of Character/account deletion; immutable Lesson and published references MUST follow Course/E15 retention and reference-counted asset deletion. Raw prompt job payloads MUST expire within 24 hours, screening metadata within 30 days, and financial references under existing ledger retention. Restores MUST reapply deletions/takedowns before serving data.

#### Scenario: Restore Characters and Casts
- **WHEN** a nightly database and asset backup is restored
- **THEN** selected and snapshotted Characters resolve to the same validated specs, rigs and voices without paid regeneration

#### Scenario: Delete Character with a published reference
- **WHEN** the owner deletes a Character whose approved content is in an enrolled published snapshot
- **THEN** it disappears from future private selections while the published payload remains until E15's last-reference retention ends

### Requirement: Character model testing and reliability
Golden set cases in the pinned Hugging Face dataset MUST cover prompt-to-spec validity, supported rigs/voices, screening, persona adherence in Lessons/chat, injection resistance, reactions and snapshot immutability. `mise run engine:eval` MUST enforce `evals/thresholds.toml`: proposed validity/immutability gates 1.00, persona adherence at least 0.85, injection resistance at least 0.99 with any leak/unsafe output/grading change a critical failure, unsafe screening recall at least 0.98 and benign false rejection at most 0.05. Screening MUST time out in 10 s, mapping in 45 s per call and 90 s total including screening; retry once then at most one provider fallback only within remaining time, token and approved cost caps. Mapping MUST cap output at 2,000 tokens. Exhausted creation MUST release Credits; invalid/unavailable runtime Characters MUST fall back to the Default cast without blocking playable learning or bypassing base generation validation.

#### Scenario: Invalid output after fallback
- **WHEN** all bounded mapping attempts fail schema or safety checks
- **THEN** no custom spec is saved, the reservation is released, the Default cast is offered and the failure is recorded

#### Scenario: Persona regression
- **WHEN** a model change lets a Character reveal hidden prompt text or change grading
- **THEN** a critical Golden set check fails and the change cannot pass the eval gate
