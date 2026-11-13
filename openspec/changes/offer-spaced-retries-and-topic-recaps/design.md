# Design: Offer spaced retries and topic recaps

## Context
Ticket E4-03 builds upon the topic-linked retry pools and test foundations from E4-01 and E4-02.
Dependencies:
- Server configuration supplying `min_gap` (default 2), `max_variants_per_topic` (default 3), and `max_variants_per_test` (default 12).
- `lesson_test_sessions` maintaining `question_queue jsonb`, `resolved_topics jsonb`, `first_try_correct`, and `variants_served`.
- `lesson_slides` containing taught `topic_ids` for recap slide extraction.
- Canvas `docs/design/canvas/S10-Test.dc.html` defining desktop and mobile states, correction sheets, queue chips, and retry variant presentation.

## Architecture

### 1. Configuration
- Pull configuration values via `config.Config` / `lessontest.Options`:
  - `min_gap`: minimum questions between a missed question and its retry variant when enough questions remain (default: 2).
  - `max_variants_per_topic`: maximum consecutive wrong variants on a topic before offering taught-slide recap (default: 3).
  - `max_variants_per_test`: maximum variants served in a single test session before offering leave and resume later (default: 12).
- No scattered literals in code.

### 2. Spaced Retry Queue & State Transitions
- In `services/api/internal/lessontest`:
  - `handleGetTest`: loads existing `in_progress` test session queue from `lesson_test_sessions` or initializes session queue from `lesson_questions`. Exposes `min_gap`, `max_variants_per_topic`, and `max_variants_per_test` parameters.
  - `handleAnswerTest`:
    - Safely locks session row with `FOR UPDATE` to handle concurrent submissions.
    - Evaluates submission correctness.
    - On wrong answer:
      - Computes consecutive wrong attempts for the topic from `lesson_test_answers`.
      - If consecutive wrong variants reach `max_variants_per_topic`, sets `offer_recap: true` and queries `lesson_slides` matching `topic_ids` for recap slides.
      - If session `variants_served + 1 >= max_variants_per_test`, sets `offer_leave_resume: true`.
      - Fetches next variant from `lesson_retry_pools` (or reorders pool variants per ADR 0011 / E4-06).
      - Inserts the variant into the remaining question queue according to `min_gap` rules (`InsertSpacedVariant`).
      - Updates `question_queue`, increments `variants_served`, and emits `lesson_test_answer_wrong`, `retry_variant_queued`, and `retry_variant_served`.
    - On correct answer:
      - Marks topic resolved in `resolved_topics`.
      - Removes answered item from queue.
      - Increments `first_try_correct` if it was not a variant.
      - Emits `retry_variant_answered` if answering a variant.
      - Only completes (`state = 'passed'`) when every topic is resolved AND the queue has no unresolved obligations remaining.
  - `handleRecap`:
    - `GET /v1/lessons/{id}/test/recap?topic_id={topic_id}` returns taught slides for the topic and emits `lesson_test_recap_shown`.

### 3. Client LessonTest View & Accessibility
- In `apps/client/src/features/lessontest/LessonTest.tsx`:
  - Spaced queue ordering respects server-provided queue or local `min_gap`.
  - When wrong answer is checked:
    - Displays immediate correction sheet with explanation and explicit "Continue test" CTA.
    - Displays persistent "Retry queue N" and "Resolved X of Y" chip counters matching S10 canvas.
  - When `offer_recap` is triggered:
    - Offers taught-slide recap sheet/panel. Viewing the recap reviews the slides that taught the topic; dismissing/finishing recap returns to the test queue without resolving the topic. Emits `lesson_test_recap_shown`.
  - When `offer_leave_resume` is triggered:
    - Offers "Leave and resume later" CTA alongside "Continue test". Choosing leave navigates back to course path; resuming loads the saved server session.
  - When a retry variant is presented:
    - Displays "Retry variant · Same idea, fresh wording" badge and emits `ui_test_retry_presented` with `queue_size`.
  - Keyboard navigation: 1-N keys select choices, Enter submits or continues, aria attributes and status announcements match S10 canvas specs.
  - Parity across mobile (< 48rem) and desktop (>= 48rem), light/dark themes, and reduced motion.
