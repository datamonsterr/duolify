# quick-notes Specification

## Purpose
Let a Learner jot a Quick note in a few seconds and have AI enrich it with a summary, tags and links, so notes are easy to read, review and track.

## Requirements

### Requirement: Create a Quick note
A Learner SHALL create a Quick note from a Lesson, a Slide or Tutor chat. The note MUST be saved immediately as raw text, before enrichment runs, and MUST work offline and sync later.

See `Raw note capture and offline queue` for related detailed requirements.

#### Scenario: Note during a Slide
- **WHEN** a Learner writes a note on a Slide
- **THEN** the note is saved with `lesson_id` and `slide_id` and is visible without waiting for AI

### Requirement: Auto-enrichment
After saving, the API SHALL enqueue an asynchronous River enrichment job that reserves daily-first eligible Credits (`quick_note` action) and calls the Engine `EnrichNote` RPC with the list of allowed course lesson references. The resulting summary, tags, and grounded lesson links MUST be stored in `quick_note_enrichments` with version and model/provider metadata, and MUST NOT overwrite learner raw text. Any lesson link not present in the course's allowed lesson references MUST be dropped. On terminal failure, reserved Credits MUST be released and status set to `enrichment_failed` while retaining the raw note.

#### Scenario: Enrichment succeeds
- **WHEN** the Engine returns summary, tags and links
- **THEN** the note shows them and `quick_note_enriched` is emitted

#### Scenario: Enrichment fails
- **WHEN** enrichment fails after retries
- **THEN** the note stays usable as raw text and the Learner may retry

#### Scenario: Grounded enrichment succeeds
- **WHEN** the Engine returns summary, tags, and valid course lesson links
- **THEN** the note displays the summary, tags, and links separately from original text
- **AND** `quick_note_enriched` and `$ai_generation` are emitted
- **AND** reserved Credits are settled

#### Scenario: Hallucinated or foreign link rejected
- **WHEN** the Engine returns a lesson link not present in the allowed course lessons
- **THEN** the foreign link is dropped before persisting enrichment
- **AND** only valid course lesson links are displayed

#### Scenario: Terminal failure preserves raw text
- **WHEN** enrichment fails or times out
- **THEN** the reservation is released with no credits charged
- **AND** the note is preserved as raw text with status `enrichment_failed`
- **AND** `quick_note_enrichment_failed` is emitted with retry available

### Requirement: Review and tracking
The Learner SHALL be able to filter notes by Course, Lesson, tag, status, and reviewed state, search note and context text, edit raw note text with optimistic revision conflict detection without overwriting prior enrichment, and mark notes as reviewed once. The client MUST show notes not yet reviewed, provide clear filter feedback, and resolve revision conflicts by displaying both versions for learner selection.

#### Scenario: Filter by tag
- **WHEN** a Learner filters by a tag
- **THEN** only notes with that tag are listed

#### Scenario: Filter by tag and unreviewed state
- **WHEN** a Learner filters by a tag or toggles "Not reviewed"
- **THEN** only notes matching the active filter criteria are listed
- **AND** the count reflects filtered results with an option to clear filters

#### Scenario: Edit raw text with optimistic concurrency
- **WHEN** a Learner edits the raw text of a note with an expected version matching the server
- **THEN** the updated raw text is saved, version increments, and prior enrichment is preserved

#### Scenario: Edit conflict detected
- **WHEN** a Learner saves an edit on a note that was modified concurrently or on another device
- **THEN** the server returns 409 Conflict with the current server version
- **AND** the client displays both the local draft and remote version, allowing the learner to keep the selected version

### Requirement: Credits
Enrichment MUST reserve and settle Credits per the `credits` spec, drawing from Daily free credits first. When no spendable Credits remain, the note MUST still be saved, enrichment MUST stay pending with status `enrichment_pending`, and the client MUST tell the Learner that enrichment is waiting and show the upsell option without blocking the note. Pending enrichments MUST run automatically, oldest first, when Credits become available, including the next day's Daily free credits or purchase top-ups. Deduplication MUST prevent double enrichment, and stale raw revisions MUST NOT be overwritten by old enrichments.

#### Scenario: Zero credits
- **WHEN** a Learner without spendable Credits saves a note
- **THEN** the note is saved with status `enrichment_pending` and the client shows that enrichment waits, with a top-up option

#### Scenario: Next day
- **WHEN** the next Daily free credits grant arrives or credits are purchased
- **THEN** pending enrichments run oldest first until the eligible credit budget is exhausted

#### Scenario: Allowance covers typical use
- **WHEN** a Learner with only the daily grant saves 3 notes
- **THEN** all 3 are enriched

#### Scenario: Stale raw revision
- **WHEN** a note is edited while pending enrichment
- **THEN** previous enrichment for the stale revision does not overwrite the updated note

### Requirement: Note analytics events
The system SHALL emit `quick_note_created`, `quick_note_enriched`, `quick_note_enrichment_failed`, `quick_note_reviewed` and `quick_note_deleted` with properties from the `analytics` spec. Note text MUST NOT be sent to PostHog.

#### Scenario: Note created
- **WHEN** a note is saved
- **THEN** `quick_note_created` is emitted with `entry_point` (`slide`, `lesson` or `chat`) and `length`

### Requirement: Note data backup and retention
Notes, enrichment, and review state MUST be stored in Postgres and covered by the nightly dump. Notes MUST be exportable by the Learner as cleanly formatted Markdown (separating original raw text from AI summary, tags, and links) and deletable by the Learner on demand or deleted with the account within 30 days.

#### Scenario: Export
- **WHEN** a Learner requests an export
- **THEN** a Markdown file with raw text, summary, tags and links is produced

#### Scenario: Export note as Markdown
- **WHEN** a Learner exports a note or note library
- **THEN** a Markdown document is generated containing raw note text, context, and separate AI additions (summary, tags, related lesson links) when present

#### Scenario: Delete note
- **WHEN** a Learner deletes a note
- **THEN** the note and its enrichment are removed from storage
- **AND** `quick_note_deleted` analytics event is emitted without note content

### Requirement: Enrichment model testing and reliability
Enrichment MUST have Golden set cases (task `note_enrich`) with deterministic checks: summary shorter than the note limit, tags within count and format, links resolve to Lessons of the same Course. It MUST use the cheap-task route with timeout and fallback from `ai-reliability`. A hallucinated link MUST fail the case.

#### Scenario: Link to unknown lesson
- **WHEN** the Engine returns a link to a Lesson not in the Course
- **THEN** the API drops the link and the eval check fails on such output

### Requirement: Raw note capture and offline queue
A Learner SHALL create a Quick note from an authorized slide, lesson, or chat source. The note MUST be saved immediately as raw text in Postgres before any enrichment queueing. Status SHALL be set to `enrichment_pending` (and may remain `enrichment_pending` at zero credit balance). Device-side drafts and offline queue MUST be distinguished from server-synced notes. Sync retries MUST use stable action keys for idempotent synchronization. Closing the note sheet or returning to the lesson MUST restore player focus and playback position.

#### Scenario: Save note during slide playback
- **WHEN** a Learner writes a note during a lesson slide
- **THEN** the note is saved with authorized lesson and slide context
- **AND** the note is saved locally before or alongside server synchronization

#### Scenario: Offline note creation and idempotent sync
- **WHEN** a note is created while offline
- **THEN** it is queued on the device with a stable action key and marked as offline draft
- **AND** when connectivity is restored, synchronizing once or multiple times creates exactly one server record

#### Scenario: Zero credit balance
- **WHEN** a note is saved by a Learner with zero spendable Credits
- **THEN** the raw note is persisted with status `enrichment_pending`
- **AND** the client indicates enrichment is pending until credits are available

#### Scenario: Restore focus on closing sheet
- **WHEN** the quick note sheet is closed
- **THEN** focus returns to the triggering lesson player control
- **AND** the lesson slide playback position is preserved

### Requirement: Chat provenance and source-unavailable notes
A Quick note saved from tutor chat SHALL carry `chat_source` with `state` `available` (thread and message ids) or `unavailable`. When the source thread is deleted the note MUST remain readable and exportable, MUST drop thread and message ids, and MUST show "source unavailable" instead of a link. Provenance MUST be included in database dumps and account export; deleted thread text MUST NOT.

#### Scenario: Source deleted
- **WHEN** the thread a note was saved from is deleted
- **THEN** the note text, summary and tags stay and its provenance reads unavailable
