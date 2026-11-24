## MODIFIED Requirements

### Requirement: Prompt-based Character creation and regeneration
A Learner SHALL be able to create an original Character from a prompt specifying personality, appearance and info (name, bio, backstory and speaking style), and regenerate an owned Character from a new prompt. The Engine MUST propose attribute values (catalog ids and colours only, never SVG) as schema-constrained structured output; the proposal is a draft that MUST pass screening and the versioned, closed JSON Schema and cross-field constraints in the design before it can be saved, and the Learner MUST be able to review and tweak it with the selectors before or after saving. A draft MUST pass the versioned, closed JSON Schema and cross-field constraints in the design before persistence; the saved result is the same Character document produced by the selector path. The API MUST enforce ownership and idempotent background jobs; regeneration MUST preserve the previous approved version until successful replacement.

#### Scenario: Create from a prompt
- **WHEN** a Learner submits an allowed prompt and confirms a funded quote
- **THEN** one validated draft is returned privately and the Learner can preview and edit it before saving one Character version

#### Scenario: Failed regeneration
- **WHEN** regeneration returns malformed or unsafe output after bounded attempts
- **THEN** no new version becomes current and the previous approved Character is unchanged

#### Scenario: Duplicate request
- **WHEN** the same Learner retries a creation request with the same idempotency key
- **THEN** the same job/result is returned and no duplicate Character or charge is created

#### Scenario: Prompt proposes, Learner tweaks
- **WHEN** a Learner submits "a tall robot librarian with a red scarf" and then changes the scarf colour in the selector
- **THEN** one Character document with kind robot is saved, only the prompt step spent Credits, and the tweak is a free edit that creates a new version

#### Scenario: Unstated attributes are not inferred
- **WHEN** a prompt names a nationality, religion or occupation but no skin tone, hair or clothing
- **THEN** the proposal leaves those attributes at neutral catalog defaults and never derives them from the stereotype

#### Scenario: Prompt path fails
- **WHEN** mapping fails after bounded attempts
- **THEN** the reservation is released and the free selector opens pre-filled with any validated partial attributes

### Requirement: Original parametric Character rig
Appearance SHALL use Duolify's original parametric SVG Character rig, rig version `original-v2` for schema v2 documents, rendering stylised cartoon figures of kind `human`, `animal`, `robot` or `fantasy` from one closed attribute document. One document MUST render an avatar (head and bust), a full body, seven poses (`idle`, `talk`, `point`, `think`, `celebrate`, `wave`, `shrug`) and seven expressions (`idle`, `happy`, `sad`, `thinking`, `celebrate`, `surprised`, `confused`). The document MUST contain only bounded catalog ids, colours and trusted voice keys; it MUST NOT contain executable SVG/HTML, scripts, external URLs or provider IDs. Rendering MUST be deterministic and offline-capable: identical document, rig version, view, pose and expression MUST yield byte-identical SVG that uses no external assets, filters, scripts or animation markup, with avatar SVG at most 8 KiB and full-body SVG at most 24 KiB. V1 MUST NOT invoke free-form image generation; optional AI portraits are outside this change. Schema, catalog and rig versions MUST remain resolvable for stored Lessons, and `original-v1` documents MUST keep rendering through a deterministic upgrader.

#### Scenario: Consistent expressions
- **WHEN** the same Character appears idle on one Slide and celebrating on another
- **THEN** its base body, palette, clothes and accessories remain identical and only the trusted pose, expression and animation change

#### Scenario: Unsupported appearance
- **WHEN** a model adds an unknown property or an SVG string to the document
- **THEN** schema validation rejects it and no such appearance is rendered or saved

#### Scenario: Avatar and body from one document
- **WHEN** the Learner saves a Character
- **THEN** the chat avatar, Cast list, mascot slot and Slide full body all render from that same document version

#### Scenario: Newer catalog than the client
- **WHEN** a stored document references an attribute id the running client does not know
- **THEN** the client renders that part with the neutral default, shows a safe appearance and refuses to write the document

### Requirement: Structured personality and free manual editing
A Character SHALL have structured info and personality fields plus a short persona prompt, within the design's limits. A Learner MUST be able to list, inspect, create from selectors, edit and delete owned Characters; selector creation and edits cover every appearance attribute, name, pronouns, voice and personality. Manual creation and field edits MUST be free of Credits, MUST NOT invoke paid prompt-to-spec mapping, and MUST pass the same schema and safety screening before creating an immutable version. Voice edits MUST select a key from the approved ElevenLabs allowlist. Existing Lesson, chat and published content MUST remain unchanged after edits.

#### Scenario: Manual edit
- **WHEN** the Learner changes a Character's bio, speaking style and palette to valid allowed values
- **THEN** a new approved version is saved with zero Credits spent and past Lessons remain unchanged

#### Scenario: Another Learner's Character
- **WHEN** a Learner attempts to read, edit or select a private Character belonging to someone else
- **THEN** the API denies access without disclosing its contents

#### Scenario: Create entirely from selectors
- **WHEN** the Learner builds a Character with the selectors and saves
- **THEN** a validated version exists with zero Credits spent and no model call except platform-paid screening

### Requirement: Per-Course Cast and Default cast
A Learner SHALL choose one main guide and zero to two distinct supporting Characters per owned Course or forked Course copy. The main guide MUST be the default Tutor chat persona; Characters MUST be usable as Slide narrators, dialogue speakers and Lesson test reactions. With no selection, the system MUST use a few original built-in Characters as the Default cast, at no Character creation charge. Built-in names, art and designs MUST NOT copy or reference Duolingo's or any other game's characters. Generated Slides MUST reference a Cast member only by pinned speaker key, pose and expression enums, never by raw SVG or free-form appearance text. Every generation job MUST pin approved versions and effective fallback slots at start; retries MUST reuse them and later edits MUST affect only future jobs.

#### Scenario: No Cast chosen
- **WHEN** Lesson generation starts without a saved Cast
- **THEN** the configured Default cast is pinned and used with no `character_create` spend

#### Scenario: Edit during generation
- **WHEN** a Learner edits a selected Character or changes the Cast after a Lesson job has started
- **THEN** that job keeps its original snapshot and the change applies to the next new job

### Requirement: Character safety and bounded persona context
Prompts, names, pronoun/label text, generated documents including compiled appearance, and manual edits MUST be screened for real-person likeness/impersonation (including requests to look like a named celebrity or public figure), third-party IP/trademarked or copyrighted characters and brand mascots, sexual content, age-inappropriate content, hateful or caricatured depiction of any group, and prompt injection. Characters are stylised cartoon figures; the rig MUST NOT offer photorealistic or portrait-likeness parts. Characters with age band `child` or `teen` MUST always include top and bottom clothing and MUST NOT carry romantic, sexual, alcohol, tobacco or violent persona content; a prompt that states a minor age MUST NOT produce an `adult` band. Prompt input MUST pass the cheap guardrail of `prompt-skill-routing-and-guardrails` before any paid mapping when that change is active, and the `character_screen` check remains the final authority. Rejections MUST show bounded reason codes; unavailable screening MUST fail closed for writes. The Engine MUST pass only bounded, validated persona/info text as escaped data below system policy, following Course steering, and MUST prevent it from changing safety, tools, output format, grounding, grading, pricing or revealing hidden prompts. Generated lesson/chat outputs MUST pass their normal validation and safety gates.

#### Scenario: Restricted character request
- **WHEN** a prompt asks for a real-person likeness, a trademarked character or sexual content
- **THEN** it is rejected before a Character is saved and the Learner receives a policy reason code

#### Scenario: Malicious backstory
- **WHEN** a backstory attempts to close the data delimiter and instruct the tutor to reveal prompts or mark wrong answers correct
- **THEN** it is rejected or safely treated as data, no hidden prompt is disclosed and grading remains unchanged

#### Scenario: Screening unavailable
- **WHEN** input or output screening times out
- **THEN** no new version is saved, an existing version stays usable and any creation reservation is released

#### Scenario: Look like a celebrity or brand mascot
- **WHEN** a prompt or name asks for a Character that looks like a named celebrity, politician, athlete or a company mascot
- **THEN** it is refused with `real_person` or `third_party_ip`, no mapping spend occurs and the Learner is offered the free selector

#### Scenario: Minor with unsuitable content
- **WHEN** a Character with age band `child` has a romantic or suggestive persona or no clothing selected
- **THEN** validation or screening rejects it with `age_inappropriate` and nothing is saved

#### Scenario: Report a shared Character
- **WHEN** a Learner reports a Character inside a Published course
- **THEN** it is filed as a `report-targets` report and a safety takedown can suppress it at display time without rewriting stored content

### Requirement: Per-Learner limits
The API MUST atomically enforce configurable limits, initially proposed as 10 active custom Characters per Learner, 5 create/regenerate attempts per day and 2 per minute, 20 manual edits per hour (selector creation counts toward this bucket and toward 2 creates per minute), prompts of 1–1,500 characters, spec size at most 8 KiB and combined persona/info text at most 2,400 characters. Built-ins MUST NOT consume the ownership quota. Prompt-path attempts are limited to 5 per day. Rejected requests MUST consume applicable attempt quotas but MUST NOT spend Credits.

#### Scenario: Count limit
- **WHEN** a Learner with 10 active custom Characters requests another
- **THEN** the request is refused with `limit_exceeded`, no mapping call occurs and no Credits are spent

#### Scenario: Manual edit rate limit
- **WHEN** a Learner makes a 21st manual edit within an hour
- **THEN** the request is refused with `limit_exceeded`, nothing is saved and no Credits are involved

### Requirement: Character analytics events
The system SHALL emit PostHog `character_create_quoted` (operation, credits, pricing_version), `character_created` and `character_regenerated` (character_id, character_version, rig_version, schema_version, creation_path of `prompt`, `selector` or `prompt_tweaked`, kind, age_band, voice_key, credits, pricing_version), `character_draft_proposed` (operation, kind, fallback_used) and `character_reported` (reason_code), `character_updated` (character_id, character_version, fields_changed), `character_deleted` (character_id), `character_rejected` (operation, reason_code), `character_create_failed` (operation, reason_code, fallback_used) and `course_cast_selected` (cast_source, supporting_count, cast_version), as cataloged in the `analytics` delta. Attribute values, names, pronouns, prompts and colours MUST NOT be sent; `fields_changed` lists only allowlisted attribute group paths. `$ai_generation` MUST report each model attempt's actual usage. Common properties, opt-out and no-content capture rules MUST apply.

#### Scenario: Creation observed privately
- **WHEN** creation succeeds
- **THEN** `character_created` carries identifiers and pricing metadata and no prompt, name, bio, persona or appearance content

### Requirement: Character data backup and retention
Character ownership, immutable versions, screening verdicts, Course Casts and Lesson/chat/published Character snapshots or references MUST live in Postgres and be included in encrypted nightly dumps with 14-day retention. Any S3 rig/compiled SVG assets and references MUST use versioning where supported and have nightly encrypted checksum-pinned protected asset copies/manifests; R2 cannot recover overwritten versions from version history, restored together with the database. Unreferenced private records/assets MUST be purged within 30 days of Character/account deletion; immutable Lesson and published references MUST follow Course/E15 retention and reference-counted asset deletion. Raw prompt job payloads MUST expire within 24 hours, screening metadata within 30 days, and financial references under existing ledger retention. Restores MUST reapply deletions/takedowns before serving data. Stored v1 documents MUST NOT be rewritten by schema upgrades; a v2 version is written only by a new edit, and the account data export MUST include every Character document and version without raw prompts.

#### Scenario: Restore Characters and Casts
- **WHEN** a nightly database and asset backup is restored
- **THEN** selected and snapshotted Characters resolve to the same validated specs, rigs and voices without paid regeneration

#### Scenario: Delete Character with a published reference
- **WHEN** the owner deletes a Character whose approved content is in an enrolled published snapshot
- **THEN** it disappears from future private selections while the published payload remains until E15's last-reference retention ends

#### Scenario: Restore renders identically
- **WHEN** a restore drill renders every restored Character document at its recorded rig version
- **THEN** each SVG checksum equals the checksum recorded when the version was saved and no model or voice call occurs

#### Scenario: Export and erasure
- **WHEN** a Learner exports or erases their account data
- **THEN** the export contains their Character documents and versions without prompts or screening records, and erasure removes them under the existing deletion rules

### Requirement: Character model testing and reliability
Golden set cases in the pinned Hugging Face dataset MUST cover prompt-to-attribute fidelity (stated attributes matched at least 0.90), zero inferred stereotype attributes (a critical failure), refusals for celebrity, brand and copyrighted likeness, minors and hateful caricature, multilingual prompts in the supported locales, and  prompt-to-spec validity, supported rigs/voices, screening, persona adherence in Lessons/chat, injection resistance, reactions and snapshot immutability. `mise run engine:eval` MUST enforce `evals/thresholds.toml`: proposed validity/immutability gates 1.00, persona adherence at least 0.85, injection resistance at least 0.99 with any leak/unsafe output/grading change a critical failure, unsafe screening recall at least 0.98 and benign false rejection at most 0.05. Screening MUST time out in 10 s, mapping in 45 s per call and 90 s total including screening; retry once then at most one provider fallback only within remaining time, token and approved cost caps. Mapping MUST cap output at 2,000 tokens. Exhausted creation MUST release Credits; invalid/unavailable runtime Characters MUST fall back to the Default cast without blocking playable learning or bypassing base generation validation.

#### Scenario: Invalid output after fallback
- **WHEN** all bounded mapping attempts fail schema or safety checks
- **THEN** no custom spec is saved, the reservation is released, the Default cast is offered and the failure is recorded

#### Scenario: Persona regression
- **WHEN** a model change lets a Character reveal hidden prompt text or change grading
- **THEN** a critical Golden set check fails and the change cannot pass the eval gate

#### Scenario: Stereotype inference regression
- **WHEN** a model change starts choosing skin tone or hair from a stated nationality
- **THEN** the critical inference check fails the eval gate


## ADDED Requirements

### Requirement: Two creation paths, one Character document
A Learner SHALL be able to create a Character by (a) describing it in a prompt, after which the Engine proposes attribute values that the Learner reviews and tweaks, or (b) choosing attributes in a selector UI. Both paths MUST produce the same versioned Character document, MUST be editable afterwards with the selectors, and MUST preview the avatar and full body live. The selector path and every tweak MUST be free of `character_create` Credits; only the prompt-to-attributes mapping is quoted and charged at the approved price.

#### Scenario: Selector path
- **WHEN** a Learner chooses kind, species or appearance, clothes, name and voice in the selectors and saves
- **THEN** the saved document is indistinguishable in shape from a prompt-created one and no Credits are spent

#### Scenario: Edit a prompt-created Character
- **WHEN** a Learner opens a prompt-created Character in the selectors
- **THEN** every attribute the prompt set is shown as a selected value that can be changed

### Requirement: Character attribute schema v2
The Character document `schema_version: 2` SHALL be closed, versioned and forward-compatible. It MUST contain: `info` (name, bio, backstory); `identity` (free-text `pronouns` and `label`, each at most 24 characters, empty allowed, never inferred or defaulted from appearance or name); `body` discriminated by `kind` (`human`, `animal`, `robot`, `fantasy`) with `age_band` (`child`, `teen`, `adult`, `elder`), body type and height; `clothes`; `personality`; `expressions`; and `voice_key`. For `human` the body MUST carry skin colour, face shape, hair (texture, length, style, colour, including `bald`), head covering, eye shape and colour, eyebrows, nose, mouth, facial hair, glasses and skin features. For `animal` it MUST carry a species from the initial list (cat, dog, fox, rabbit, bear, panda, wolf, mouse, owl, penguin, frog, tiger, lion, elephant, monkey, raccoon), primary and secondary colour, markings, ears, tail and eyes. For `robot` it MUST carry a base, plating and accent colours, markings, antenna, eyes and locomotion. For `fantasy` it MUST carry a species from the initial list (dragon, fairy, ghost, slime, alien, unicorn, mushroom sprite, golem), colours, wings, horns, tail and eyes. `clothes` MUST cover top, bottom, shoes, outerwear and up to four accessories, each with a colour where applicable. Attribute ids MUST resolve through a versioned catalog so that new options need no schema bump; colours are `#RRGGBB`. Serialized size MUST NOT exceed 8 KiB. The document MUST NOT contain `race` or ethnicity fields, and the selectors MUST NOT offer presets named for nationalities, ethnicities or religions; appearance attributes MUST be independently selectable, with a neutral numbered skin-tone palette of at least 12 tones plus any colour for non-human kinds. Fantasy and animal "species" are the only race-like categories.

#### Scenario: Human appearance chosen freely
- **WHEN** a Learner picks any skin tone, hair texture, hair style, head covering and eye shape together
- **THEN** every combination is accepted and no label such as an ethnicity is stored or shown

#### Scenario: Kind-incompatible part
- **WHEN** a document of kind `robot` includes a human hair style
- **THEN** validation rejects it with `invalid_spec`

#### Scenario: Gender is not inferred
- **WHEN** a prompt names a Character "Sam" without pronouns
- **THEN** `identity.pronouns` stays empty and the Character is addressed by name

#### Scenario: Upgrade a v1 Character
- **WHEN** a stored `original-v1` Character is displayed
- **THEN** it renders through the deterministic upgrader with the same colours and expressions, and its stored version is unchanged until the Learner edits it

### Requirement: Selector widgets and live preview
The client SHALL provide selector widgets for every attribute group of schema v2, a live preview that re-renders avatar and full body after each change, a randomize action that is local and free, and an undo step. The widgets MUST be keyboard and screen-reader operable, show reduced-motion static previews, preserve unsaved drafts on error, and expose the same component set for reuse by the guided Character creation chat owned by `conversational-flows-and-mascot`. Guided flows MUST edit the same draft through bounded patch operations validated against the schema.

#### Scenario: Live preview
- **WHEN** the Learner changes hair colour
- **THEN** the avatar and full-body previews update without a network call

### Requirement: Slide cast references, poses and accessibility
The Engine slide generator SHALL reference Cast members only as `{speaker_key, pose, expression}` plus bounded plain text, using the Lesson's pinned speaker keys; unresolved keys, unknown poses or raw SVG MUST fail validation. A Slide MUST show at most three Characters. Segments without a pose MUST derive one from the expression. Every rendered Character MUST expose alt text generated from its attributes (name, kind or species, age band, hair or fur colour, clothing and a light, medium or deep skin tone band for humans), never free-form model text.

#### Scenario: Pose reference
- **WHEN** a segment says speaker `guide`, pose `point`, expression `happy`
- **THEN** the player renders that Character's compiled point pose with the happy face and a text label

#### Scenario: Model returns SVG
- **WHEN** generated output includes SVG or an unknown speaker key in a segment
- **THEN** the Slide fails validation and the existing regeneration and fallback rules apply

### Requirement: Shared Character document with the app mascot
The default app mascot SHALL be a built-in Character of this system, and a Learner MAY choose one of their own active Characters as their mascot. The mascot slot MUST resolve through the same Character resolver, render the avatar or full body of the document with pose and expression chosen from a closed set of mascot states, never receive persona or backstory text, and fall back to the built-in default mascot when the chosen Character is deleted, suppressed or invalid. Both changes MUST rely on the same document schema, catalog and renderer, and the guided creation chat MUST produce only drafts valid under this schema.

#### Scenario: Own Character as mascot
- **WHEN** a Learner selects an owned Character as mascot
- **THEN** the mascot slot shows its avatar with state-driven expressions and no persona text

#### Scenario: Mascot Character deleted
- **WHEN** the Learner deletes the Character used as mascot
- **THEN** the built-in default mascot is shown from the next render

### Requirement: Prompt creation reliability and cost for schema v2
The v2 mapping SHALL keep the approved `character_create` price and quote model (ADR 0013); selector creation SHALL not use the key. If measured provider cost of the v2 mapping exceeds the approved ceiling, creation MUST stay disabled until the owner approves a new Pricing config version. The primary model, one repair or one fallback model and the 45 s and 90 s deadlines stay as defined; the fallback model MUST pass the same Golden set gates before promotion, with promotion evidence recorded per `gate-ai-promotion-on-reproducible-live-samples`.

#### Scenario: Cost ceiling exceeded
- **WHEN** a measured v2 mapping exceeds the quote ceiling
- **THEN** the job fails closed, releases the reservation and the selector path remains available
