# Generate and pass the first gated lesson

## Why
E4-01 implements the first end-to-end tracer from the E2 course path to the E3 lesson player and server-graded test, recording the authoritative completion consumed by E5. Gated generation requires that only eligible lessons (the first lesson, or a lesson whose predecessor is passed) can be generated, with paid version-pinned reservations and SSE job state. Learners play Motion HTML slides and must pass every topic in a server-graded test without completion bypass.

## What Changes
- Enqueue lesson generation as an idempotent River background job with version-pinned reservation and SSE streaming stages.
- Validate engine output: slides Motion HTML, duration bounds, plain-text narration, and original test questions without revealing answers or explanations before submission.
- Narrow tracer maps each validated original question to one API-owned topic; wrong answers reveal correction and queue a safe reordered-source fallback until all topics resolve correctly.
- Authoritative passing commits completion and completion outbox once; reaching the last slide cannot bypass grading.
- Client LearnPath renders ready-to-generate estimate, preparing SSE job card, and start node; LessonPlayer transitions to server test; LessonTest provides keyboard/touch quiz with instant feedback and finish transition matching canvas specs S08, S09, S10, S12.
- Telemetry emits allowlisted events (`lesson_generation_requested`, `lesson_generated`, `lesson_test_started`, `lesson_test_submitted`, `lesson_test_passed`, `$ai_generation`) without learner text.
- Persist lesson metadata, slides, questions, test sessions and completion outbox in Postgres with nightly dump, data inventory, backup fixtures and 30-day account deletion.
- AI reliability: add `lesson_slides`, `lesson_test`, `lesson_narration` validation and eval cases in golden-set fixtures; pass `mise run engine:eval`.

## Impact
API lessontest package, database migrations, engine validation and evals, client player and test routes, data inventory and backup fixtures. No retry variants/topic pools (E4-02), spaced retries (E4-03), resume (E4-04), or next lesson prep (E4-05).
