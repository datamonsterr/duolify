# Design: Generate topic-linked retry pools with lessons

## Context
Ticket E4-02 replaces the narrow one-topic-per-question tracer mapping from E4-01 with engine-generated topic linkages and pre-generated retry variant pools.
Dependencies:
- Proto contract in `proto/duolify/engine/v1/engine.proto`.
- API `lessontest` service, migrations, and test grading.
- Engine lesson generation, question validation, and variant generation.
- Owner decision (ADR 0011, E4-06): when a retry pool is exhausted, reuse the pool reordered for free (no paid on-demand variants at launch).

## Architecture

### 1. Proto Contract
- `Slide`: gains `repeated string topic_ids = 5;`.
- `Question`: gains `string topic_id = 6;` and `string variant_of = 7;`.
- `TopicVariants`: `string topic_id = 1;` and `repeated Question variants = 2;`.
- `Lesson`: gains `repeated TopicVariants variant_pool = 4;`.
- `GenerateRetryVariants`: RPC added to `EngineService` with `GenerateRetryVariantsRequest` and `GenerateRetryVariantsResponse`.

### 2. Database Schema
- `lesson_slides`: add `topic_ids text[] NOT NULL DEFAULT '{}'::text[]`.
- `lesson_questions`: add `variant_of text`.
- `lesson_retry_pools`: stores private pre-generated variants per lesson stub and topic:
  - `id uuid PRIMARY KEY DEFAULT gen_random_uuid()`
  - `lesson_stub_id uuid NOT NULL REFERENCES course_lesson_stubs(id) ON DELETE CASCADE`
  - `learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE`
  - `topic_id text NOT NULL`
  - `variants jsonb NOT NULL`
  - `created_at timestamptz NOT NULL DEFAULT now()`
  - `UNIQUE(lesson_stub_id, topic_id)`

### 3. Engine and AI Reliability
- `engine_core::lessontest`:
  - Enforce non-empty `topic_ids` on slides.
  - Enforce non-empty `topic_id` on questions.
  - Enforce taught-topic coverage: every question's `topic_id` must match a topic taught by at least one slide.
  - Enforce variant pool quality: prompt must differ from source and prior variants; choices must be paraphrased or reordered without duplicates; correct answer semantic equality with source answer; answer index in bounds.
- Golden set:
  - Add task `lesson_test_variant` with paraphrase, semantic equality, and duplicate choice cases.
  - Golden set cases evaluate with deterministic checks and judge pass criteria.

### 4. API Service & Session Flow
- Lesson generation job validates slides, questions, taught-topic coverage, and variant pools.
- Private pools are stored in `lesson_retry_pools` without sending answers to client.
- `GET /v1/lessons/{id}/test`: returns client-safe public questions (`question_id`, `topic_id`, `prompt`, `choices`).
- `POST /v1/lessons/{id}/test/answer`:
  - Grades answer.
  - On wrong answer: records attempt, reveals explanation and answer_index, queues retry variant from private pool.
  - Variant spacing: inserted after `min_gap = 2` remaining questions where possible.
  - If pool exhausted: reorders pool variants for free without extra engine charge (ADR 0011).
  - Emits allowlisted telemetry events (`lesson_test_answer_wrong`, `retry_variant_queued`, `retry_variant_served`, `retry_variant_answered`).

### 5. Client
- Match canvas `S10-Test.dc.html`:
  - Variant badge: `<span className="rv">Retry variant · Same idea, fresh wording</span>`.
  - Status chips: `Resolved X of Y` and `Retry queue Z`.
  - Immediate correction sheet with `Not quite — here's why`, explanation, and continue action.
  - Keyboard/touch interaction (1-9 to select choice, arrow keys, Enter to Check/Continue).
  - Responsive desktop/mobile, light/dark themes, reduced-motion compliance.
