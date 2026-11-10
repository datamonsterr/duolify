# Design: Handle exhausted retry pools without blocking tests

## Architecture & Flow

### 1. Free Retry Reuse & Safe Fallback
Per ADR 0011 (owner decision E4-06):
- When a learner answers a test question incorrectly, the API looks up the topic's retry pool from `lesson_retry_pools`.
- If unused variants remain in the pool, the next unused variant is served.
- When the pool is exhausted (all pool variants have already been served or queued):
  - The API recycles the approved pool variant with reordered choices (`ReorderChoices`).
  - No provider or engine RPC call is made; no credits or reservation are needed, keeping tests usable for learners with zero credit balance.
  - The API emits allowlisted event `retry_variant_generation_failed` with `topic_id` and `error_code: "pool_exhausted"` (no learner text).
  - The API emits `retry_variant_served` with `source: "pool_exhausted_fallback"`.
- If the pool is missing, empty, or invalid:
  - Safe source fallback: the original source question for that topic is reused with reordered choices (`ReorderChoices`).
  - The API emits `retry_variant_generation_failed` with `topic_id` and `error_code: "safe_source_fallback"`.
  - The API emits `retry_variant_served` with `source: "safe_source_fallback"`.
- The new choice order and updated answer index are calculated deterministically.

### 2. Never Auto-Pass & Recap Limits
- The recycled variant is inserted into the session queue respecting `min_gap` spacing.
- The learner MUST answer the served variant correctly to mark the topic resolved. Auto-passing is strictly forbidden.
- If consecutive wrong answers on a topic reach `max_variants_per_topic` (default 3), taught recap slides are returned in `recap_slides` and `offer_recap: true`.
- If total variants served in a session reach `max_variants_per_test` (default 12), `offer_leave_resume: true` is returned.

### 3. Persisted Provenance and Stored Instruction Snapshot
- Table `lesson_test_variant_snapshots` persists every served variant:
  - `id` (uuid PK)
  - `session_id` (uuid FK to `lesson_test_sessions`)
  - `learner_id` (uuid FK to `learners`)
  - `lesson_stub_id` (uuid FK to `course_lesson_stubs`)
  - `topic_id` (text)
  - `question_id` (text)
  - `variant_of` (text)
  - `source` (text)
  - `reason` (text nullable)
  - `prompt` (text)
  - `choices` (jsonb)
  - `answer_index` (integer)
  - `explanation` (text)
  - `instruction_snapshot` (jsonb) retaining the original lesson context/instructions snapshot
  - `created_at` (timestamptz)
- When grading an answer, the API consults `lesson_test_variant_snapshots` first, ensuring that recycled choices and new answer index are correctly scored.
- Covered by nightly backup dumps, privacy deletion (30 days cascade), and data inventory.

### 4. Client Recovery & Canvas S10-Test Fidelity
- Matches Canvas S10-Test "Variant failure · offline waiting" state:
  - When variant answer submission or preparation fails:
    - Display alert banner: "Couldn't prepare the retry question / Your progress and queue are saved. No credits are charged for retries."
    - Action controls: primary "Retry" button (re-submits the check without penalty) and secondary "Return to lesson" button.
  - Zero credits state: tests continue without showing paid checkout or blocking progress.
  - Desktop and mobile layouts: keyboard shortcuts (1-9, Enter), touch targets, dark/light theme, and reduced-motion support.

### 5. Telemetry & Analytics
- Allowlisted events: `retry_variant_generation_failed` (`course_id`, `lesson_id`, `topic_id`, `error_code`) and `retry_variant_served` (`course_id`, `lesson_id`, `topic_id`, `source`).
- Never send learner text or question prompt/choices to PostHog.
