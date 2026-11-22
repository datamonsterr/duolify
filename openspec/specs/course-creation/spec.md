# course-creation Specification

## Purpose
Let a Learner create a Course on any topic with an AI agent. The Learner sees a Course preview first, then the agent generates only the Course outline. Lesson content is generated later, one Lesson at a time, under Gated generation.

## Requirements

### Requirement: Course preview before generation
The system SHALL show a Course preview (a sample Course outline and example Slides) for a topic before the Learner commits to creating a Course. The API MUST call the Engine `PreviewCourse` RPC and MUST NOT create a Course record during preview.

#### Scenario: Learner requests a preview
- **WHEN** a Learner submits a topic and asks for a preview
- **THEN** the API returns a sample Course outline and example Slides
- **AND** no Course or enrollment row is created

#### Scenario: Learner discards the preview
- **WHEN** a Learner leaves the preview without confirming
- **THEN** nothing is persisted except the preview analytics event

### Requirement: Outline-only generation
After explicit final cost consent, the system SHALL create an outline-only River job from the latest learner-confirmed edited outline revision. This replaces older read-only/sample-outline wording: ordered units, lesson titles/goals, additions and removals MUST be preserved exactly; unapplied proposals MUST NOT enter the job. The private GenerateCourseOutline request MUST carry the edited outline, draft revision and trusted quote bounds. Output containing Slides or Lesson tests or differing from the confirmed structure MUST be rejected within the bounded evaluated fallback policy.

#### Scenario: Learner confirms a preview
- **WHEN** a Learner confirms course creation
- **THEN** the API stores the Course, its Units and its Lesson stubs with status `locked`
- **AND** the first Lesson is marked `ready_to_generate`
- **AND** the Learner is enrolled in the Course

#### Scenario: Outline response contains lesson content
- **WHEN** the Engine returns an outline that contains Slides
- **THEN** the API MUST reject the response and retry or fall back

#### Scenario: Confirmed edited outline creates a course
- **WHEN** the learner accepts the revision-bound quote and confirms Create
- **THEN** validated success atomically stores the Course, ordered Units, locked Lesson stubs and one enrollment
- **AND** only the first Lesson is ready_to_generate and no lesson content is generated

#### Scenario: Stale or malformed output
- **WHEN** the revision changed before submission or output changes authored structure or includes lesson content
- **THEN** stale submission is rejected before provider work or invalid output fails bounded validation
- **AND** terminal failure releases undelivered reserved Credits and leaves no partial enrollment

### Requirement: Gated lesson generation
The system SHALL generate Lesson N+1 only after the Learner passes the Lesson test of Lesson N. The API MUST refuse to call `GenerateLesson` for a Lesson whose predecessor has no recorded pass. The `GenerateLesson` call MUST include the Learner's active Course instructions as defined in the `course-steering` capability, and the generated Lesson MUST record which instructions it used.

#### Scenario: Previous test not passed
- **WHEN** a client asks to generate a Lesson and the previous Lesson test is not passed
- **THEN** the API responds with a conflict error and the Engine is not called

#### Scenario: Previous test passed
- **WHEN** the Learner passes the Lesson test of Lesson N
- **THEN** the API enqueues a generation job for Lesson N+1
- **AND** progress is streamed to the client through SSE

#### Scenario: Active instructions
- **WHEN** the generation job for Lesson N+1 starts and the Learner has active instructions
- **THEN** the Engine call carries them and the Lesson stores their ids

### Requirement: Generation is a background job
Generation SHALL run as a River job, never on the request path. A job MUST be idempotent per Lesson: a retry MUST NOT create a second Lesson or charge Credits twice. Outline creation jobs MUST support authenticated SSE reconnection, automatic terminal failure recovery on deadline expiry, and retry only with fresh quote consent. GET and SSE reads SHALL NOT transition jobs or settle Credits. A periodic worker reaper SHALL expire abandoned queued/generating creations and pending proposals using expected-state guards. Validated creation recovery SHALL idempotently complete persistence without repeating provider work, including legacy settled creations with no Course. New settlement SHALL atomically persist the Course, Units, Lesson stubs and enrollment in the Credits transaction. River cancellation, worker restart, and duplicate job deliveries MUST not produce duplicate enrollments or double charges.

#### Scenario: Worker restarts mid-generation
- **WHEN** the worker restarts while a Lesson job runs
- **THEN** the job resumes or retries and the Lesson exists exactly once
- **AND** the Credits reservation is settled once

#### Scenario: Worker restarts
- **WHEN** a worker restarts after validation or settlement
- **THEN** it resumes the same stored output and finalization with exactly one enrollment and settlement
- **AND** an interrupted provider call without durable output fails closed with released undelivered Credits

#### Scenario: Creation worker restarts or disconnects
- **WHEN** a client disconnects or worker restarts during course creation
- **THEN** the client reconnects to the existing job via SSE without charging again
- **AND** if the job terminated or timed out, reserved credits are released and explicit retry is offered with a fresh quote

### Requirement: Credits for generation
Course preview, outline generation and Lesson generation SHALL reserve Credits before the Engine call and settle them with the reported `Usage`, as defined in the `credits` spec.

#### Scenario: Insufficient credits
- **WHEN** a Learner has fewer Credits than the estimate
- **THEN** the API refuses the request with `insufficient_credits` and the Engine is not called

### Requirement: Course analytics events
The API SHALL emit the PostHog events `course_preview_requested`, `course_preview_viewed`, `course_outline_generated`, `course_enrolled`, `lesson_generation_requested`, `lesson_generated` and `course_generation_failed` with the properties defined in the `analytics` spec.

#### Scenario: Outline generated
- **WHEN** an outline is stored
- **THEN** `course_outline_generated` is emitted with `course_id`, `unit_count`, `lesson_count`, `provider`, `model` and `latency_ms`

### Requirement: Course data backup and retention
Course, Unit, Lesson stub, enrollment and generated Lesson rows MUST live in Postgres and be covered by the nightly dump. Slide HTML MUST live in object storage with versioning. Deleting a Course MUST soft-delete first and hard-delete after 30 days with unreferenced creation and asset record cleanup.

#### Scenario: Restore drill
- **WHEN** the restore drill restores the latest dump
- **THEN** every Course has its Units and Lesson stubs, and Slide object keys resolve in storage

#### Scenario: Owner deletes course
- **WHEN** the authenticated owner deletes a Course
- **THEN** `deleted_at` is set, the Course is immediately hidden from course listings and foreign requests return 404
- **AND** if active, `active_course_id` is cleared

#### Scenario: Course purge after 30 days
- **WHEN** the purge job runs on courses soft-deleted older than 30 days
- **THEN** the Course, Units, Stubs, Enrollments and unreferenced Creations are purged from Postgres

### Requirement: Course model testing and reliability
Course preview and outline generation MUST have Golden set cases (task `course_preview`, `course_outline`) and MUST pass `mise run engine:eval`. The Engine SHALL apply a per-call timeout, retry once, then fall back to the OpenRouter provider. The outline MUST pass deterministic checks: valid structure, no empty titles, no Slides.

#### Scenario: Primary provider times out
- **WHEN** the Claude API call exceeds the timeout twice
- **THEN** the Engine retries on the fallback provider and reports it in `Usage.provider`

#### Scenario: Eval gate
- **WHEN** a prompt change lowers a task score under its threshold in `evals/thresholds.toml`
- **THEN** `engine:eval` fails

### Requirement: Re-plan credits
Re-planning the remaining outline SHALL reserve Credits before the Engine call and settle them with the reported `Usage`, at the Pricing config version in the quote, as defined in the `credits` spec.

#### Scenario: Re-plan charged
- **WHEN** a re-plan proposal is produced
- **THEN** a `spend` entry is written with the quote's Pricing config version

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

### Requirement: Clarify and explicitly confirm a Course brief
The system SHALL collect an editable Course brief through one-question guided chat or an equivalent form before paid preview. Confirmed onboarding context MAY seed fields; skipped context MUST stay empty. Each field SHALL support free text, quick replies and Skip with at most one follow-up for vague answers. Include and skip topics MUST remain separate.

#### Scenario: Review on either screen size
- **WHEN** a learner clarifies their intent
- **THEN** desktop shows a docked editable brief and mobile shows an equivalent sheet with confirm and keep-chatting actions
- **AND** no preview, outline, enrollment, reservation or Course is created

#### Scenario: Assistant unavailable
- **WHEN** chat is disabled, offline or times out
- **THEN** all collected answers and the current answer are available in the equivalent form
- **AND** the learner can confirm without chat or a positive balance

### Requirement: Immutable authorized brief revisions
The API SHALL persist a learner-owned immutable revision only on explicit confirmation, with canonical hash and 30-day expiry. Changes after confirmation MUST invalidate the local confirmation and require a new revision. Reads MUST enforce ownership and expiry. Revisions SHALL enter account exports and encrypted general backups retained 14 days, cascade on erasure and honor restore tombstones. Session drafts MUST NOT be server-backed.

#### Scenario: Edit after confirmation
- **WHEN** a confirmed brief is edited
- **THEN** its local confirmation is cleared and the stored revision remains unchanged

#### Scenario: Another learner reads a revision
- **WHEN** a different authenticated learner requests the revision
- **THEN** the API returns not found without revealing the brief

### Requirement: Platform-paid development brief chat
Chat SHALL read course_brief_chat price, bounds and platform caps from Pricing config. Development SHALL allow the explicitly unapproved zero-credit config independently of launch approval. Production MUST require approved config and evaluated model promotion; the form MUST remain usable. Chat MUST NOT deduct learner Credits and SHALL be rate limited.

#### Scenario: Production gate closed
- **WHEN** the unapproved development config is selected in production
- **THEN** chat is disabled and form entry remains available

### Requirement: Brief chat analytics and reliability
The system SHALL use existing analytics wrappers with authenticated UUID, common properties and opt-out checks, never learner text. The catalogue SHALL include confirm_brief, course_brief sheet opening and existing guided controls. Every provider attempt SHALL report actual usage where available. Course brief chat SHALL enforce configured deadlines below API deadlines, token, attempt and USD caps, validate model schema deterministically and fall back to form on terminal failure. Offline Golden set cases SHALL exercise the production brief processor under mock providers and pass engine:eval; live publication remains gated by ADR 0011 provisioning.

#### Scenario: Invalid provider output
- **WHEN** the model returns an invalid schema
- **THEN** bounded attempts end in the form fallback with the draft retained

#### Scenario: Analytics opted out
- **WHEN** a learner disables analytics
- **THEN** brief controls and model attempts send no PostHog event

### Requirement: Confirmed brief preview consent
A paid personalized preview MUST use an owned unexpired confirmed brief revision and an accepted fresh quote bound to that revision. Brief edits MUST invalidate preview consent. Clarifying answers and quote acceptance alone MUST NOT generate or reserve. Insufficient paid Credits MUST cause zero Engine calls.

#### Scenario: Changed brief
- **WHEN** a preview request names a different revision from its accepted quote
- **THEN** the API rejects it before reserving or calling the Engine

#### Scenario: Paid preview delivery
- **WHEN** a Learner confirms the brief, reviews the versioned paid-only quote and explicitly consents
- **THEN** the API loads that exact revision and current profile, reserves through E7-03 and returns validated sample outline and sandboxed example Slides without Course or enrollment creation

### Requirement: Ephemeral preview reliability and recovery
The flow SHALL show Brief → Preview → Outline → Create with draft-preserving Back, discard, shop return and failure recovery. Regeneration SHALL require fresh quote and consent. The Engine SHALL validate nonempty outline titles/goals and safe slides, apply configured deadlines and at most the accepted bounded evaluated fallback within quote/USD limits. Terminal failure SHALL release undelivered Credits. Preview output SHALL remain ephemeral; existing 30-day confirmed brief and seven-year financial retention/export/deletion policies SHALL apply without a new learner table. Nightly backup and restore SHALL retain financial records and brief expiry/erasure behavior. API analytics SHALL emit course_preview_requested, course_preview_viewed and course_generation_failed with lengths, identifiers and reason codes only, honoring opt-out; each provider attempt SHALL report actual usage through $ai_generation. Offline course_preview Golden set cases SHALL include beginner, expert, empty and non-English profiles; live promotion remains gated by E10-06.

#### Scenario: Malformed primary
- **WHEN** the primary returns a structurally invalid preview
- **THEN** one evaluated fallback may run only within the accepted remaining cost and deadline

#### Scenario: Terminal failure or discard
- **WHEN** generation fails or a delivered preview is discarded
- **THEN** undelivered reservations are released on failure, the draft remains available and neither path creates Course or enrollment rows

### Requirement: Free accessible sample outline editing
Learners SHALL reorder units and lessons using drag handles or move buttons, rename, edit goals, add/remove items and undo. IDs SHALL remain stable, order SHALL follow arrays and lessons SHALL have exactly one parent. Position announcements and focus SHALL remain on moved items. Mobile and desktop SHALL expose identical operations with token-based light/dark and reduced-motion presentation. Manual editing and undo MUST NOT call a model or spend Credits.

#### Scenario: Undo after reload
- **WHEN** a learner saves edits and reloads the saved outline
- **THEN** the same IDs, order, content and last 50 pre-edit snapshots are restored
- **AND** Undo restores the preceding snapshot

### Requirement: Authorized draft revision concurrency
The API SHALL validate ownership, schema and expected_revision before saving or confirming. Invalid saves SHALL preserve client edits. Stale saves SHALL preserve the current server head and persist the submitted draft as a recovery copy for explicit choice. Drafts and bounded history SHALL join account export, account/brief deletion and encrypted nightly 14-day general backups with restore tombstones applied.

#### Scenario: Concurrent stale save
- **WHEN** two clients save the same expected revision
- **THEN** one advances the head and the other receives a conflict with both saved branches
- **AND** neither client silently replaces the other branch

### Requirement: Confirm exact outline snapshot without generation
Explicit confirmation SHALL expose draft ID, revision, canonical hash and exact outline snapshot for later course creation and proposal validation. Any later edit SHALL invalidate current confirmation. Confirmation MUST NOT create a Course, enroll, call the engine or reserve Credits. UI events SHALL use the existing private-text-free allowlisted analytics wrapper and catalogue controls outline_move, outline_rename, outline_remove, outline_add and outline_undo, respecting opt-out.

#### Scenario: Stale consumer
- **WHEN** a consumer requests a confirmed revision after the draft changed
- **THEN** the API rejects it and requires explicit confirmation of the newer revision

### Requirement: Quoted immutable sample-outline proposals
The system SHALL quote `outline_chat_change` from versioned Pricing config, display eligibility and require consent before reservation. It SHALL send the owned edited draft/revision and untrusted request to the stateless engine. Insufficient eligible Credits SHALL cause zero engine calls. The immutable proposal SHALL contain validated added, removed, moved or retitled operations and SHALL NOT change the draft on generation. Production prices SHALL remain gated by E12-09/E12-10 approval.

#### Scenario: Deliver then discard
- **WHEN** a learner consents to an eligible quote and a valid proposal is delivered
- **THEN** the existing reservation settles within the quote ceiling once
- **AND** Discard changes no outline and never refunds or starts generation

#### Scenario: Provider failure or cancellation
- **WHEN** bounded evaluated retries and fallback fail before delivery
- **THEN** the reservation is released, the draft and request remain recoverable and reconnect reads the same proposal ID

### Requirement: Fresh atomic proposal decisions
Apply SHALL compare the immutable base revision and atomically write one new draft revision with bounded undo. Discard and Undo SHALL be free and idempotent. A stale proposal SHALL preserve manual edits and require explicit re-quote. Desktop docked chat and mobile step SHALL expose identical labelled diffs and recovery actions.

#### Scenario: Stale or duplicate Apply
- **WHEN** a proposal is applied twice or the draft has been manually changed
- **THEN** a duplicate returns its recorded result and a stale proposal refuses to overwrite the draft

### Requirement: Private recoverable proposal lifecycle
The API SHALL allowlist and catalogue proposal lifecycle events before emission, honor opt-out and emit identifiers and reason codes only. Proposal data SHALL export and erase with its learner, expire with its brief within 30 days and join encrypted nightly 14-day backups with erasure replay. Financial records SHALL retain their separate existing policy. GET reads SHALL never settle or fail proposals. A periodic worker reaper SHALL release expired pending proposals and recover validated delivery. All state transitions SHALL compare the expected state and check affected rows; settlement and delivery/failure SHALL commit atomically so stale workers and reapers cannot overwrite each other. Offline mock-provider golden cases SHALL gate deterministic diff validity, manual edit preservation, injection, insufficient balance and failed reservation release.

#### Scenario: Restore and erase
- **WHEN** a draft and proposal are restored or the learner is deleted
- **THEN** restored proposals require no provider work and deletion removes private proposal content through the existing erasure policy

### Requirement: Confirmed creation data and reliability
Creation snapshots, Courses, Units, Lesson stubs and enrollment SHALL be covered by nightly backup fixtures, learner export and account erasure policies in the inventory. Course deletion SHALL retain the existing 30-day soft-delete grace. Analytics SHALL emit course_outline_generated, course_enrolled, course_generation_failed and $ai_generation through allowlisted wrappers without learner text. Mock-provider course_outline Golden cases SHALL check structure, empty titles, no Slides/tests, authored revisions and profile handling and pass engine:eval; live promotion remains E10-06.

#### Scenario: Restore and privacy
- **WHEN** a backed-up enrolled outline is restored
- **THEN** its order, goals, first-ready status and single enrollment survive
- **AND** account export includes its data and account deletion erases learner content via cascades

### Requirement: Authorized course enrollment reads and switching
The API SHALL expose an authorized `GET /v1/courses` endpoint returning the authenticated Learner's enrolled Courses with confirmed Units, ordered Lesson stubs with goals, and level overrides. Access to foreign, soft-deleted or non-existent Courses MUST return a neutral not found status without leaking enrollment or existence. Active course selection SHALL be persistent in Postgres and survive client restart.

#### Scenario: Learner lists enrolled courses
- **WHEN** an authenticated Learner requests `/v1/courses`
- **THEN** the API returns all confirmed Courses enrolled by the Learner with their ordered Units, Lessons, goals and current selection

#### Scenario: Foreign course access is neutral
- **WHEN** a Learner attempts to read or select a Course they are not enrolled in
- **THEN** the API responds with a neutral 404 status

#### Scenario: Selection persists
- **WHEN** a Learner selects a Course
- **THEN** that Course becomes active and remains selected across client restarts

#### Scenario: Invalid level input preserves the override
- **WHEN** a level change body is empty, malformed, oversized, contains unknown fields, omits the level, or supplies an invalid enum
- **THEN** the API returns 400 and preserves the existing override; an explicit empty level string resets it

#### Scenario: Deleted course cannot be selected or re-levelled
- **WHEN** a learner selects or changes the level of a soft-deleted Course
- **THEN** the API returns the same neutral 404 as a foreign Course and preserves selection and override

### Requirement: Creation flow and outline recovery
The system SHALL preserve confirmed briefs, working manual outline edits, undo history, and applied proposal diffs across page reloads, job retries, and returns from the credits shop. The client SHALL restore the furthest authorized step in the Brief → Preview → Outline → Create funnel. Stale proposals or failed suggestion requests MUST leave the existing outline intact and MUST NOT silently resubmit chat edits or charge on reconnect.

#### Scenario: Return from credits shop
- **WHEN** a learner navigates to purchase credits and returns to course creation
- **THEN** the confirmed brief, manual outline edits, and creation step are restored without data loss

#### Scenario: Stale proposal recovery
- **WHEN** an outline proposal failed or timed out during disconnect
- **THEN** the outline remains intact, reserved proposal credits are released, and no proposal changes are silently applied

#### Scenario: Persistence fails after validation
- **WHEN** course persistence fails after the engine response is validated
- **THEN** the settlement and product writes roll back together, and periodic recovery retries the durable response with exactly one enrollment and charge

#### Scenario: Expiry competes with a worker
- **WHEN** an expired creation or proposal is read while its worker and the reaper compete
- **THEN** reads leave state and Credits untouched, and only the expected-state winner can settle delivery or release failure

### Requirement: Purged Course objects are deleted from storage
When the purge job hard-deletes a Course it MUST record the Course's private generated-lesson object prefixes (`lessons/generated/<id>/`: slide HTML and manifest) in the same transaction, so a crash cannot lose them. A later run MUST delete those objects only after 15 days (longer than the 14-day nightly dumps, so a restorable dump never points at deleted objects), MUST skip a prefix any live lesson manifest still references, and MUST retry on a storage failure. Published snapshots (`lessons/published/…`) and deduplicated narration audio MUST NOT be deleted by this path: they keep their own reference-counted retention (`shared_asset_retention`, `narration_assets`).

#### Scenario: Purged Course objects removed after retention
- **WHEN** a Course is purged and the retention period passes
- **THEN** its generated slide and manifest objects are deleted, objects of non-purged Courses and published snapshots remain, and a repeat run is a no-op

#### Scenario: Storage fails during object deletion
- **WHEN** object deletion fails
- **THEN** the ledger row stays pending and the next daily run retries it

### Requirement: Failed deletes and reads never look like success
Soft deletion and the active-Course reset MUST commit in one transaction. A failed read of the active Course MUST return 503, not an empty selection.

#### Scenario: Active-course reset fails
- **WHEN** clearing `active_course_id` fails during Course deletion
- **THEN** the request returns 503 and the Course stays undeleted

### Requirement: Authored shared discovery example
The system SHALL serve a fixed safe versioned authored sample outline and Slides at zero balance without engine/provider calls, Course or enrollment creation, reservations or Credit grants. The shared example SHALL be clearly distinguished from a personalized paid preview. Live catalogue activation SHALL remain gated by E12-08; development configuration SHALL remain unapproved.

#### Scenario: Inspect a shared example
- **WHEN** a visitor or Learner opens the shared example from Welcome or Create
- **THEN** the authored outline and sandboxed Slides are displayed without personal input or paid generation events
- **AND** no Course, enrollment, quote, reservation or grant is written

### Requirement: Preserve creation while inspecting the demo
The client SHALL preserve guided brief answers, composer and edited creation drafts when entering or leaving the shared demo. Returning to personalized creation SHALL require a fresh quote and explicit consent before any paid preview work.

#### Scenario: Resume personalized creation
- **WHEN** a Learner returns from the shared example
- **THEN** their Brief → Preview → Outline → Create draft remains available
- **AND** previous quote consent cannot start a personalized preview

### Requirement: Shared demo analytics and recovery
The catalogue SHALL include course_preview_viewed with preview_kind=shared_demo and demo_version, and ui_control_activated with control_id=shared_demo and surface=welcome|course_brief. Events SHALL use UUID/common properties, respect opt-out and contain no learner text. Shared demo assets SHALL be Git-backed and covered by checksum-verified versioned object-storage restore fixtures. Existing AI eval/fallback gates SHALL remain unchanged.

#### Scenario: Demo at zero balance or offline
- **WHEN** a Learner with zero balance opens the demo or its download fails
- **THEN** no paid event or credit effect occurs and the client offers Return and Retry without losing drafts

#### Scenario: Restore authored assets
- **WHEN** current demo objects are lost
- **THEN** the protected versioned copy restores the exact Git-authored bundle without AI generation or learner data

### Requirement: Backed-off creation event polling
The course-creation event stream SHALL read creation state at 200 ms after it opens or the state changes, doubling the interval up to 3 seconds while the state is unchanged. Heartbeats and the terminal `enrolled` and `failed` events SHALL be unchanged.

#### Scenario: Idle generation
- **WHEN** a creation stays in one state for a minute
- **THEN** the stream reads it fewer than 40 times and still sends heartbeats

### Requirement: Course list summaries
`GET /v1/courses` SHALL return the authenticated Learner's confirmed enrolled Courses as summaries including card progress, without a unit or Lesson tree. The detail route SHALL provide the tree. The list SHALL be private and revalidated with an ETag.

#### Scenario: List courses
- **WHEN** an authenticated Learner requests the Course list
- **THEN** the response contains Course summaries and the active Course id without full unit or Lesson trees
