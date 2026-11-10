# Design: Find review and export quick notes (E9-04)

## Context
E9-01 implemented raw quick note saving and offline syncing. E9-02 implemented grounded AI enrichment with summaries and tags. E9-04 implements the complete note library experience independent of AI: multi-facet filtering (course, lesson, tag, status, unreviewed), text search, editing raw text with optimistic revision conflict detection, marking reviewed once, note deletion, Markdown export, and design canvas parity for desktop list-detail and mobile two-screen states.

## Architecture and Data Model

### 1. Database Schema Extensions
- **`quick_notes` Table Additions**:
  - `version integer NOT NULL DEFAULT 1`: increments on every raw text update for optimistic locking.
  - `reviewed_at timestamptz`: records the exact instant a note was marked reviewed.
- **Indexes**:
  - `CREATE INDEX quick_notes_learner_reviewed ON quick_notes(learner_id, reviewed);`
  - `CREATE INDEX quick_notes_learner_course ON quick_notes(learner_id, course_id);`

### 2. API Endpoints
- **`GET /v1/notes`**:
  - Query parameters:
    - `course_id`: filter by course UUID
    - `lesson_id`: filter by lesson UUID
    - `tag`: filter by enrichment tag (matched against `quick_note_enrichments.tags`)
    - `status`: filter by status enum (`enrichment_pending`, `enriched`, etc.)
    - `reviewed`: boolean string (`true` or `false`)
    - `q`: search query string matched against `raw_text`, `slide_title`, and enrichment `summary`/`tags`
  - Returns array of `NoteDTO` with `version`, `reviewed_at`, and enrichment details.
- **`PATCH /v1/notes/{id}`**:
  - Request body:
    - `raw_text`: updated text (1-2000 characters)
    - `expected_version`: integer version for optimistic concurrency
    - `reviewed`: optional boolean to set reviewed status
  - Concurrency behavior:
    - If `expected_version` matches current `quick_notes.version`, updates `raw_text`, increments `version`, updates `updated_at`, and preserves prior enrichment.
    - If `expected_version` does not match, returns HTTP 409 Conflict with the current server `NoteDTO`.
- **`POST /v1/notes/{id}/review`**:
  - Idempotently marks `reviewed = true` and records `reviewed_at = now()`.
  - Emits `quick_note_reviewed` analytics event.
- **`DELETE /v1/notes/{id}`**:
  - Deletes note record from Postgres (cascading to `quick_note_enrichments`).
  - Emits `quick_note_deleted` analytics event.
- **`GET /v1/notes/{id}/export` and `GET /v1/notes/export`**:
  - Returns generated Markdown representation formatted per canvas specifications.

### 3. Client Architecture (`apps/client/src/features/notes`)
- **Desktop List-Detail and Mobile Two-Screen Navigation**:
  - Desktop: List pane (420px) on left with search, filter chips, result count, and clear filters; detail pane on right (`dkm`).
  - Mobile (< 48rem): Screen 1 (note list with search & filter chips); clicking a note transitions to Screen 2 (note detail with "Back to notes" button).
- **Edit & Conflict Resolution UI**:
  - Note detail provides an "Edit" button that reveals an inline editor for raw text.
  - When saving an edit, `expected_version` is passed.
  - On 409 Conflict, displays the conflict resolution banner:
    - "This note changed on another device. Pick which version to keep. Nothing is lost until you choose."
    - Two radio options: Local draft vs Server version.
    - "Keep selected" button to finalize the chosen version.
- **Markdown Export**:
  - "Export Markdown" button downloads or copies Markdown.
  - Emits `ui_control_activated` with `control_id: "export_notes"`.
- **Delete and Review Actions**:
  - "Mark reviewed" button sets reviewed status.
  - "Delete" button removes note with confirmation.
- **States & Accessibility**:
  - Loading, empty ("Keep an idea from your next lesson..."), no-results ("0 notes match... Clear filters"), offline/error ("Waiting to sync"), long content, keyboard/focus, light/dark, and reduced motion.
