# Prepare only the next lesson after a pass

## Why
E4-01 and E4-02 established the initial gated generation and topic-linked test grading tracer. Ticket E4-05 joins authoritative completion to the N+1 River workflow and learning history. Passing Lesson N enqueues only Lesson N+1; missing predecessor pass refuses before any engine call; duplicate passes create no duplicate jobs or charges. Completed lesson summaries (including weak topics) are persisted and provided as `history` (oldest first) alongside effective learner profile and course outline to the stateless engine. Next lesson preparation failure or insufficient credits must never undo an already completed lesson.

## What Changes
- Enqueue only Lesson N+1 upon passing Lesson N with confirmed quote policy; duplicate pass submissions are idempotent and create no second job or charge.
- Verify predecessor pass strictly: attempting to prepare Lesson N+1 without an authoritative pass of Lesson N refuses with 409 Conflict before any engine call.
- Persist completed-lesson summaries in Postgres with weak topics (topics missed during the test session) and completed timestamp.
- Stateless Engine integration: send course outline, current effective learner profile, and completed-lesson history (oldest first) with weak topics to `GenerateLesson`.
- Failure and affordability isolation: next lesson generation failure or insufficient credit balance releases reservations and marks generation failed, but leaves Lesson N authoritative completion ('done') intact.
- Finish and path UI matching S12-Finish and S08-Path canvas states:
  - S12-Finish: shows completion stats (+XP, streak), next lesson status (preparing if paid, needs credits banner with "See credit options", or failure banner with "Try again · new estimate", and "Back to learn path").
  - S08-Path: completed (done/review), current/ready, generating, needs credits (violet gate), generation failed with retry. Parity for touch and keyboard.
- Analytics: emit allowlisted events (`lesson_test_passed`, `lesson_generation_requested`, `lesson_generated`, `course_generation_failed`, `$ai_generation`) with UUID and common properties, opt-out, and zero learner text.
- Data backup and retention: cover completed lesson summaries and job references in nightly dumps and restore verification; erase on account deletion within 30 days.
- AI reliability: add gated lesson/history/empty-profile and all-provider-failure golden set cases; pass `mise run engine:eval`.

## Impact
API lessontest package, database migrations, engine prompt context, client LearnPath and LessonTest finish screens, golden set evals, data inventory and backup fixtures.
