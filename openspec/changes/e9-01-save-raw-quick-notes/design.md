# Design: Save raw quick notes before enrichment (E9-01)

## Context
E9 introduces quick notes and bookmarks. E9-01 specifically establishes the raw note persistence and capture foundation before AI enrichment or bookmarks (E9-05) are added.

## Architecture and Data Model
1. **Postgres Table `quick_notes`**:
   - `id uuid PRIMARY KEY DEFAULT gen_random_uuid()`
   - `learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE`
   - `action_key text NOT NULL` (client-generated idempotency key, unique per learner)
   - `course_id uuid REFERENCES courses(id) ON DELETE SET NULL`
   - `lesson_id uuid REFERENCES course_lesson_stubs(id) ON DELETE SET NULL`
   - `slide_id integer` (slide index/offset in the lesson deck)
   - `slide_title text` (snapshot label of the slide)
   - `source text NOT NULL CHECK(source IN ('slide', 'lesson', 'chat'))`
   - `raw_text text NOT NULL CHECK(length(trim(raw_text)) > 0 AND length(raw_text) <= 2000)`
   - `status text NOT NULL DEFAULT 'enrichment_pending' CHECK(status IN ('enrichment_pending', 'enriching', 'enriched', 'enrichment_failed', 'raw_only'))`
   - `reviewed boolean NOT NULL DEFAULT false`
   - `created_at timestamptz NOT NULL DEFAULT now()`
   - `updated_at timestamptz NOT NULL DEFAULT now()`
   - `UNIQUE(learner_id, action_key)`

2. **API Handlers (`/v1/notes`)**:
   - `POST /v1/notes`: Create / synchronize a raw quick note.
     - Validates source: if `lesson_id` is supplied, checks that the lesson exists and belongs to the learner or an enrolled course.
     - Validates raw text: non-empty, trimmed, <= 2,000 characters.
     - Idempotent: if `action_key` already exists for this learner, returns the existing record.
     - Emits `quick_note_created(source, length)` without learner note text.
   - `GET /v1/notes`: Lists quick notes for authenticated learner.
   - `GET /v1/notes/{id}`: Returns a specific quick note.

3. **Client Offline Device Queue and UI**:
   - Immediate device save into `localStorage`/in-memory queue with stable `action_key`.
   - Distinct banners for:
     - Local device save (offline): "Offline variant · saved on this device. It will sync once you're online. Not backed up yet."
     - Synced server save: "Saved. Enrichment is pending. Synced to your account. The AI summary and tags appear in Quick notes."
     - Zero-credit notice: "Zero-credit variant. Your note is saved. Add credits to enrich it, or wait for your daily reset."
   - Restores player focus and resume position when closing panel or clicking "Back to lesson".

4. **Privacy, Retention, and Backup**:
   - `quick_notes` is classified as `erased` on learner deletion within 30 days.
   - Included in nightly database backup dump.
   - Exported in `/v1/me/data-export`.
   - Analytics events `quick_note_created` and `ui_sheet_opened` never leak note text.
