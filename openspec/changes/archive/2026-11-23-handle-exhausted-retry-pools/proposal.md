# Handle exhausted retry pools without blocking tests

## Why
Under ADR 0011 (owner decision E4-06), when a retry pool for a topic is exhausted, the system reuses the pool (reordered) for free, with no on-demand paid AI variants at launch. This ensures that zero-balance learners are never blocked from completing lesson tests. If a retry pool is missing or invalid, the API returns unavailable without consuming the answer or echoing the source question (review #161). The API retains the lesson instruction snapshot, and strictly require the learner to answer correctly rather than auto-passing.

## What Changes
- Enforce the owner-approved free reuse exhaustion path: recycle approved pool items with reordered choices and no provider/engine call, keeping tests fully usable at zero balance.
- Return 503 unavailable, without consuming the answer, when the retry pool is missing or invalid; never serve the source prompt as a variant.
- Persist served variant provenance and instruction snapshots into `lesson_test_variant_snapshots` in Postgres, covered by nightly dumps and account deletion erasure within 30 days.
- Emit allowlisted analytics: `retry_variant_pool_exhausted` (with `topic_id` and `error_code`, without learner text) and `retry_variant_served` (with `topic_id` and `variant_source`). `retry_variant_generation_failed` stays allowlisted but is not emitted: there is no runtime generation.
- Never auto-pass: the learner must answer the recycled variant correctly to pass the topic.
- Obey recap limits (`max_variants_per_topic` consecutive failures on a topic triggers recap slides) and leave/resume limits (`max_variants_per_test`).
- Match canvas S10-Test desktop and mobile recovery states: display variant failure alert ("Couldn't prepare the retry question / Your progress and queue are saved. No credits are charged for retries.") with explicit Retry and Return to lesson recovery actions, with keyboard and touch accessibility.
- Update golden-set fixtures with exhaustion, failure, and paraphrase cases passing `mise run engine:eval`.

## Impact
API lessontest service and database persistence, client LessonTest component and Playwright e2e suite, engine-core validation, data inventory documentation, and analytics telemetry.
