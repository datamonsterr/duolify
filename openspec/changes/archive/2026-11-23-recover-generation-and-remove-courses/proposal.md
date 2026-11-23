# Recover generation and remove courses

## Why
E2-04 completes the Course creation lifecycle and error recovery across the full Brief → Preview → Outline → Create flow. When connection drops or workers fail, confirmed briefs, manual outline edits and applied proposals must survive without duplicate enrollments or undisclosed charges. Failed creation jobs must release reservations and offer explicit retry with a fresh quote. Learners can also remove courses they own with an immediate hide and a 30-day soft-delete grace before permanent purge.

## What Changes
- Restore current creation step and authorized brief/outline revision on reconnect; keep manual edits and applied diff state through reload, job retry and shop returns.
- Connect authenticated SSE to active outline creation job; on terminal failure, release reserved credits and allow explicit retry only with a fresh accepted quote.
- Handle River job retries, worker restarts, cancellation, and duplicate delivery safely with advisory locking and idempotent enrollment.
- Expose owner-only `DELETE /v1/courses/{id}` soft-deleting the course immediately, clearing active selection if active, and purging expired soft-deleted courses after 30 days with unreferenced record cleanup.
- Add allowlisted analytics: catalogue `ui_recovery_selected` and verify `course_generation_failed` properties, respecting opt-out and sending no private text.
- Match desktop and mobile canvas specifications from S04-Create and S03-Courses with accessible confirmation, keyboard focus, light/dark modes, and reduced-motion support.

## Impact
API courses service, router and worker migrations; database course soft deletion and purge; client courses, catalog and creation flow; analytics allowlist and data inventory. No new paid external provider credentials required.
