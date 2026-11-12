# Design: Bookmark lessons and exact slides (E9-05)

## Context
E9 introduces quick notes and bookmarks. E9-05 implements unique learner-target bookmarks for lessons and exact slides, enabling one-tap save/remove, offline replay, course filtering, and authorized deep linking.

## Architecture and Data Model
1. **Postgres Table `bookmarks`**:
   - `id uuid PRIMARY KEY DEFAULT gen_random_uuid()`
   - `learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE`
   - `action_key text NOT NULL` (client-generated idempotency key, unique per learner)
   - `course_id uuid REFERENCES courses(id) ON DELETE SET NULL`
   - `lesson_id uuid REFERENCES course_lesson_stubs(id) ON DELETE SET NULL`
   - `slide_id integer CHECK(slide_id IS NULL OR slide_id >= 0)`
   - `target_type text NOT NULL CHECK(target_type IN ('slide', 'lesson'))`
   - `course_title text NOT NULL DEFAULT ''`
   - `lesson_title text NOT NULL DEFAULT ''`
   - `slide_title text NOT NULL DEFAULT ''`
   - `created_at timestamptz NOT NULL DEFAULT now()`
   - `updated_at timestamptz NOT NULL DEFAULT now()`
   - `UNIQUE (learner_id, action_key)`
   - `UNIQUE NULLS NOT DISTINCT (learner_id, lesson_id, slide_id)`

2. **API Handlers (`/v1/bookmarks`)**:
   - `POST /v1/bookmarks`: Create or idempotently synchronize a bookmark.
     - Validates target: `lesson_id` authorized for the learner; `target_type` in ('slide', 'lesson'); if 'slide', `slide_id >= 0`.
     - Idempotent: duplicate replay with same action key or same target returns existing bookmark.
     - Emits `bookmark_added(target_type, course_id, lesson_id, slide_id)`.
   - `DELETE /v1/bookmarks/{id}` or `POST /v1/bookmarks/remove`: Remove bookmark by ID or target.
     - Emits `bookmark_removed(target_type, course_id, lesson_id, slide_id)`.
   - `GET /v1/bookmarks`: Lists bookmarks for authenticated learner, optionally filtered by `?course_id=`.
     - Evaluates availability: joins `course_lesson_stubs` and `courses` to flag whether target is available or deleted.
   - `GET /v1/bookmarks/{id}`: Returns specific bookmark with availability status.

3. **Client UI and Offline Queue**:
   - Offline queue persists add/remove actions locally with stable action keys.
   - Replaying offline queue syncs each action once without duplicates.
   - Bookmarks view (`/bookmarks`) provides:
     - Course filter chips ("All courses", course names).
     - Cached slide cards with "Saved offline" chip and "Open" button linking to `/lesson/$lessonId?slide=$slideId`.
     - Uncached slide cards with "not downloaded" notice and offline recovery banner.
     - Deleted targets labelled "This saved slide is unavailable" (or "This saved lesson is unavailable") with a Remove button (no Open button, never silently redirected).
   - Lesson player integration:
     - Bookmark tool toggle in player controls (desktop button and mobile tool bar).
     - One-tap add/remove with toast feedback ("Slide bookmarked" with "Undo" link).

4. **Privacy, Retention, and Backup**:
   - `bookmarks` is classified as `erased` on learner deletion within 30 days.
   - Included in nightly database backup dump.
   - Exported in `/v1/me/data-export`.
   - Analytics events `bookmark_added`, `bookmark_removed`, and `bookmark_opened` never leak private learner text.
