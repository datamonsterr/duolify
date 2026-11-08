# quick-notes Specification

## Purpose
Let a Learner jot a Quick note in a few seconds and have AI enrich it with a summary, tags and links, so notes are easy to read, review and track.

## Requirements

### Requirement: Create a Quick note
A Learner SHALL create a Quick note from a Lesson, a Slide or Tutor chat. The note MUST be saved immediately as raw text, before enrichment runs, and MUST work offline and sync later.

#### Scenario: Note during a Slide
- **WHEN** a Learner writes a note on a Slide
- **THEN** the note is saved with `lesson_id` and `slide_id` and is visible without waiting for AI

### Requirement: Auto-enrichment
After saving, the API SHALL enqueue an enrichment job that calls the Engine `EnrichNote` RPC. The result (summary, tags, links) MUST be stored next to the raw text and MUST NOT overwrite it.

#### Scenario: Enrichment succeeds
- **WHEN** the Engine returns summary, tags and links
- **THEN** the note shows them and `quick_note_enriched` is emitted

#### Scenario: Enrichment fails
- **WHEN** enrichment fails after retries
- **THEN** the note stays usable as raw text and the Learner may retry

### Requirement: Review and tracking
The Learner SHALL be able to filter notes by Course, Lesson and tag, search text, and mark notes as reviewed. The client MUST show notes not yet reviewed.

#### Scenario: Filter by tag
- **WHEN** a Learner filters by a tag
- **THEN** only notes with that tag are listed

### Requirement: Credits
Enrichment MUST reserve and settle Credits per the `credits` spec. When Credits are insufficient, the note MUST still be saved and enrichment MUST stay pending until Credits are available.

#### Scenario: Zero credits
- **WHEN** a Learner without Credits saves a note
- **THEN** the note is saved with status `enrichment_pending`

### Requirement: Note analytics events
The system SHALL emit `quick_note_created`, `quick_note_enriched`, `quick_note_enrichment_failed`, `quick_note_reviewed` and `quick_note_deleted` with properties from the `analytics` spec. Note text MUST NOT be sent to PostHog.

#### Scenario: Note created
- **WHEN** a note is saved
- **THEN** `quick_note_created` is emitted with `source` (`slide`, `lesson` or `chat`) and `length`

### Requirement: Note data backup and retention
Notes, enrichment and review state MUST be stored in Postgres and covered by the nightly dump. Notes MUST be exportable by the Learner as Markdown and deleted with the account within 30 days.

#### Scenario: Export
- **WHEN** a Learner requests an export
- **THEN** a Markdown file with raw text, summary, tags and links is produced

### Requirement: Enrichment model testing and reliability
Enrichment MUST have Golden set cases (task `note_enrich`) with deterministic checks: summary shorter than the note limit, tags within count and format, links resolve to Lessons of the same Course. It MUST use the cheap-task route with timeout and fallback from `ai-reliability`. A hallucinated link MUST fail the case.

#### Scenario: Link to unknown lesson
- **WHEN** the Engine returns a link to a Lesson not in the Course
- **THEN** the API drops the link and the eval check fails on such output
