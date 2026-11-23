# Design: Prepare only the next lesson after a pass

## Context
Ticket E4-05 connects the lesson test pass to the N+1 generation workflow and learning history.
Dependencies:
- Authoritative lesson completion and outbox (`lesson_completion_outbox`).
- River background job queue (`lessons` queue).
- Credits service with reservations, claim, settle, release.
- Pricing service quotes and quote consent.
- Engine stateless `GenerateLessonRequest` with `outline`, `lesson`, `learner`, `history`, `limits`.
- Canvas designs: `docs/design/canvas/S12-Finish.dc.html` and `docs/design/canvas/S08-Path.dc.html`.

## Architecture

### 1. Database Schema
New migration `20261110160000_lesson_completion_summaries.sql`:
- Table `lesson_completion_summaries`:
  - `id uuid PRIMARY KEY DEFAULT gen_random_uuid()`
  - `learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE`
  - `course_id uuid NOT NULL REFERENCES courses(id) ON DELETE CASCADE`
  - `lesson_id uuid NOT NULL REFERENCES course_lesson_stubs(id) ON DELETE CASCADE`
  - `summary text NOT NULL CHECK(length(trim(summary))>0)`
  - `weak_topics jsonb NOT NULL DEFAULT '[]'::jsonb`
  - `completed_at timestamptz NOT NULL DEFAULT now()`
  - `UNIQUE(learner_id, lesson_id)`
- Indexes on `learner_id`, `course_id`, `(course_id, completed_at)`.

### 2. Pass and N+1 Enqueue Workflow
When a learner passes Lesson N in `handleAnswerTest`:
1. Transactionally:
   - Mark `lesson_test_sessions` state = 'passed'.
   - Mark current stub (`lessonID`) status = 'done'.
   - Collect weak topics from `lesson_test_answers` where `correct = false` for this session.
   - Insert summary into `lesson_completion_summaries` on conflict do nothing.
   - Insert into `lesson_completion_outbox` on conflict do nothing.
   - Identify next lesson stub N+1 in course order.
   - If next lesson stub N+1 exists:
     - Mark stub status = 'ready_to_generate' (or 'generating' if enqueued).
2. For N+1 preparation:
   - When client requests preparation (`POST /v1/lessons/{id}/prepare`) or if auto-enqueued:
     - Verify predecessor pass: verify that the immediate predecessor lesson stub is marked 'done' and has a record in `lesson_completion_outbox`. If not, return 409 Conflict without calling the engine.
     - Validate quote (`lesson_content`, accepted, not expired).
     - Idempotent insert into `lesson_generations` with unique active constraint or check.
     - Enqueue River job `lesson_generation`.

### 3. Stateless Engine Context
When `RunLessonGeneration` runs:
1. Retrieve course outline (all units and lesson stubs) for `course_id`.
2. Retrieve current effective learner profile from `learner_profiles`.
3. Retrieve all completed lesson summaries for this learner in this course ordered by `completed_at ASC` (oldest first).
4. Populate `GenerateLessonRequest`:
   - `Outline`: course outline
   - `Lesson`: target lesson stub ref
   - `Learner`: learner profile
   - `History`: slice of formatted completed lesson summary strings (oldest first), e.g. "Lesson 1: Introduction to Stars. Passed. Weak topics: fusion, light."
   - `Limits`: usage limits from financial reservation.
5. In case of generation failure or insufficient credits:
   - Release reservation, mark `lesson_generations` state = 'failed', return stub to 'ready_to_generate'.
   - Crucially: do NOT change Lesson N's completion status ('done'). Lesson N remains permanently completed.

### 4. Client Parity (S12-Finish & S08-Path)
- `LessonTest` finish screen:
  - Hero checkmark icon.
  - Heading "Lesson complete".
  - Score / XP and streak status (`+15 XP · 8-day streak`).
  - Next lesson state:
    - If auto-preparing or ready: displays preparing progress.
    - If needs credits: violet banner "Next lesson needs X credits" / "Purchased balance: Y. Your progress is saved; prepare it whenever you're ready." with link/button "See credit options".
    - If generation failed: danger banner "Failure variant · next lesson couldn't be prepared" / "Reserved credits were released. This lesson is still complete." with button "Try again · new estimate".
    - Button "Back to learn path".
  - Keyboard and touch navigation.
- `LearnPath`:
  - Renders completed lessons with checkmark / review link.
  - Renders N+1 with estimate / prepare button when ready, blue progress spinner when preparing, violet banner when credits needed, retry button when failed.
  - Matches tokens in `docs/design/tokens.json`.

### 5. Telemetry & AI Reliability
- Allowlisted events: `lesson_test_passed`, `lesson_generation_requested`, `lesson_generated`, `course_generation_failed`, `$ai_generation`.
- No learner text; common properties: `source`, `platform`, `app_version`, `course_id`, `lesson_id`.
- AI eval: golden cases in `golden.sample.jsonl` testing `lesson_slides`, `lesson_test`, `lesson_test_variant`, `lesson_narration` with history and empty profiles.
