# Handle exhausted retry pools without blocking tests

## Why
Under ADR 0011 (owner decision E4-06), when a retry pool for a topic is exhausted, the system reuses the pool (reordered) for free, with no on-demand paid AI variants at launch. This ensures that zero-balance learners are never blocked from completing lesson tests. If a retry pool is unavailable, insufficient, or invalid, the API must fall back safely to the source question with reordered choices, emit failure telemetry without learner text, retain the lesson instruction snapshot, and strictly require the learner to answer correctly rather than auto-passing.

## What Changes
- Enforce the owner-approved free reuse exhaustion path: recycle approved pool or source questions with reordered choices and no provider/engine call, keeping tests fully usable at zero balance.
- Fall back safely to the source question when retry pool entries are unavailable, exhausted, or invalid.
- Persist served variant provenance and instruction snapshots into `lesson_test_variant_snapshots` in Postgres, covered by nightly dumps and account deletion erasure within 30 days.
- Emit allowlisted analytics: `retry_variant_generation_failed` (with `topic_id` and `error_code`, without learner text) and `retry_variant_served` (with `topic_id` and `source`).
- Never auto-pass: the learner must answer the recycled variant correctly to pass the topic.
- Obey recap limits (`max_variants_per_topic` consecutive failures on a topic triggers recap slides) and leave/resume limits (`max_variants_per_test`).
- Match canvas S10-Test desktop and mobile recovery states: display variant failure alert ("Couldn't prepare the retry question / Your progress and queue are saved. No credits are charged for retries.") with explicit Retry and Return to lesson recovery actions, with keyboard and touch accessibility.
- Update golden-set fixtures with exhaustion, failure, and paraphrase cases passing `mise run engine:eval`.

## Impact
API lessontest service and database persistence, client LessonTest component and Playwright e2e suite, engine-core validation, data inventory documentation, and analytics telemetry.
