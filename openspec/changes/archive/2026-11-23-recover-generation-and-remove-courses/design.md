# Design

## Resilient Creation Funnel Recovery
1. **Durable Handoff & Session State**:
   - The Brief → Preview → Outline → Create flow tracks the learner's current step in session storage (`course-creation-step:${owner}`).
   - On reconnect or page return (e.g. from `/credits`), the flow restores the furthest authorized step:
     - Step 1 (Brief): unconfirmed brief drafts remain in session storage; confirmed briefs survive via `course_brief_revisions`.
     - Step 2 (Preview): previously previewed samples remain accessible without re-charging.
     - Step 3 (Outline): manual outline edits, undo history, and applied proposal diffs are preserved via working outline cache and `outline_drafts`. Stale proposal state or failed attempts leave the base outline intact without silent resubmission.
     - Step 4 (Create): confirmed outline revision and active creation job IDs are tracked. Reconnect restores the creation card and reattaches SSE.
2. **Creation Job Reconnect & Retry Lifecycle**:
   - `GET /v1/course-creations/{id}` and `/v1/course-creations/{id}/events` allow reconnecting to an ongoing or finished creation job.
   - If a job worker died or exceeded its deadline while in `queued` or `generating`, reading status triggers terminal recovery: undelivered reservations are released and the job is marked `failed`.
   - On terminal failure, reserved credits return to the wallet and the learner is offered an explicit "Try again" action.
   - Retry requires fresh consent: fetching a new quote (`POST /v1/course-creations/quote`), learner acceptance, and posting a new creation submission.
   - Storing the new quote resets the failed creation job into `queued` state and enqueues a new River job without changing draft identity or creating duplicate enrollments.
   - River advisory locks prevent concurrent execution, and duplicate deliveries immediately exit early if already `enrolled` or `failed`.

## Owner-Only Course Removal & 30-Day Purge
1. **Soft Delete (`DELETE /v1/courses/{id}`)**:
   - Authenticated owner verification: queries `courses` matching `id` and authenticated `learner_id`. Foreign or unauthenticated requests return 404 with neutral responses, preventing enumeration.
   - Soft-delete timestamp: sets `courses.deleted_at = now()`. The course is immediately excluded from `GET /v1/courses` and `GET /v1/courses/{id}`.
   - If the deleted course was the active course on `learner_profiles.active_course_id`, the active course selection is reset.
2. **Purge After 30-Day Grace**:
   - `PurgeDeletedCourses(ctx, cutoff)` identifies soft-deleted courses where `deleted_at <= cutoff` (default 30 days).
   - Deletion cascades through `course_enrollments`, `course_units`, `course_lesson_stubs`, and corresponding unreferenced `course_creations`.
   - Any unreferenced slide/object references are cleanly purged while preserving referenced materials.

## Design Board Parity (S03-Courses and S04-Create)
1. **Creation Surface (S04)**:
   - Stepper: `1 Brief`, `2 Preview`, `3 Outline`, `4 Create` with active (`now`), completed (`done`), and standard states.
   - Failed state: matches canvas banner with "Couldn't create it", "Your 55 reserved credits were released. Your edits are saved.", and "Try again" action.
   - In-progress state: persistent job card "Preparing your outline… You can come back."
2. **Courses Surface (S03)**:
   - Course cards include an accessible destructive "Remove course" control with a confirmation dialog:
     - Identifies course title, explains 30-day grace and immediate hiding.
     - Cancel button and explicit Delete action with focus management (Escape to dismiss, focus return).
   - Unavailable courses display "Course unavailable" with "The owner removed this course. Your notes stay in Quick notes."

## Analytics & Privacy
- Emitters: `course_generation_failed` (API) on terminal creation failure and `ui_recovery_selected` (Client/API) on error retry or resume.
- Allowlist: add `ui_recovery_selected` with safe properties (`surface`, `error_code`, `action`, `course_id`, `screen_id`) to `services/api/internal/analytics/analytics.go`.
- Opt-out: respected by both client and server; no private prompt, outline text, course title or goal is ever sent.
