# Design: Generate and pass the first gated lesson

## Context
Ticket E4-01 implements the first gated lesson generation and test flow.
Dependencies:
- Course outline with first lesson stub marked `ready_to_generate`.
- Pricing config providing `lesson_content` (140 credits) and `narration` bounds.
- Credits service reserving, settling and releasing reservations.
- Engine generating slides with valid Motion HTML, plain-text narration and test questions.

## Architecture

### 1. Database Schema
New tables:
- `lesson_generations`: Tracks lesson generation jobs, state (`queued`, `generating`, `validated`, `completed`, `failed`), quote ID, reservation ID, and error codes.
- `generated_lessons`: Holds generated lesson metadata, authored ID, title, and manifest JSON.
- `lesson_slides`: Stores validated Motion HTML, duration ms, and narration text.
- `lesson_questions`: Stores multiple choice questions, API-owned `topic_id`, choices JSON, `answer_index`, and `explanation`.
- `lesson_test_sessions`: Tracks in-progress and completed test attempts, queue of pending questions, and resolved topics.
- `lesson_test_answers`: Records each submitted answer attempt, chosen index, correctness, and timestamp.
- `lesson_completion_outbox`: Authoritative completion entry consumed by E5.
- Update `course_lesson_stubs` status constraint to permit `'generating'`, `'ready'`/`'current'`, `'done'`.

### 2. API Endpoints
- `POST /v1/lessons/{id}/prepare` (or `POST /v1/lesson-generations`): Initiates lesson generation job with accepted quote, reserving credits.
- `GET /v1/lesson-generations/{id}/events`: SSE stream reporting stage changes (`queued`, `generating`, `validating`, `ready`, `failed`).
- `GET /v1/lessons/{id}`: Returns lesson title and slides (`slide_id`, `html`, `duration_ms`, `narration`). Does not reveal test questions, answers or explanations.
- `GET /v1/lessons/{id}/test`: Returns test questions (`question_id`, `topic_id`, `prompt`, `choices`) without `answer_index` or `explanation`.
- `POST /v1/lessons/{id}/test/answer`: Submits `{ question_id, choice_index }`. Evaluates answer server-side, records in session, returns correction (`correct`, `answer_index`, `explanation`, `resolved_count`, `total_topics`, `retry_queue_count`, `passed`).
- On test pass: updates lesson stub to `done`, marks next lesson stub `ready_to_generate`, writes to `lesson_completion_outbox`, emits `lesson_test_passed`.

### 3. Engine and AI Reliability
- `engine_core::lessontest`: validates slides HTML, narration constraints (plain text, speaking rate / duration bounds), question structure (choices >= 2, valid answer_index, non-empty explanation).
- Eval runner: validates `lesson_slides`, `lesson_test`, `lesson_narration` offline fixtures, enforcing deterministic schema and critical checks.

### 4. Client
- `LearnPath`: renders `ready_to_generate` node with credit estimate; clicking triggers quote acceptance and generation; SSE job card shows stages; turns into green Start node upon readiness.
- `LessonPlayer`: loads lesson from API; navigation across slides; final slide button labeled "Take lesson test" navigating to `/lesson/$lessonId/test`. Reaching last slide cannot bypass test.
- `LessonTest`: renders questions matching S10 canvas specs with keyboard 1-N/Enter support; immediate feedback sheet on wrong/correct; reordered retry fallback; navigates to Finish on completion.
- `Finish`: renders completion confirmation and link back to course path matching S12 canvas specs.
