# Generate topic-linked retry pools with lessons

## Why
E4-01 established the initial lesson generation, player delivery, and test completion tracer. However, questions were only mapped to synthetic API-owned topics, and wrong answers merely reordered choices of the exact same question. E4-02 implements explicit topic IDs, slide-to-topic coverage mapping, and pre-generated retry variant pools within the lesson pipeline. When learners make mistakes, retry variants with paraphrased prompts, reordered choices, and preserved semantics are served after other questions without incurring extra engine latency or charges.

## What Changes
- Extend `Question` and `Slide` contract in proto with `topic_id`, slide `topic_ids`, `variant_of` provenance, and `variant_pool` on `Lesson`. Add `GenerateRetryVariants` RPC.
- Engine generates 2 pre-generated variants per topic alongside the lesson. Each variant features paraphrased prompts, reordered choices, and verified semantic equality with the source question.
- API validates that every slide teaches at least one topic, every question belongs to exactly one topic, all question topics are covered by slides, choices contain no duplicates, and variant answers are privately stored in Postgres.
- Test sessions serve retry variants with a minimum gap (`min_gap = 2` where possible). When pool variants are exhausted, ADR 0011 / E4-06 reuses the pool reordered for free.
- Analytics emits allowlisted events (`lesson_test_answer_wrong`, `retry_variant_queued`, `retry_variant_served`, `retry_variant_answered`, `retry_variant_generation_failed`, `lesson_test_recap_shown`) without learner text.
- Match canvas S10-Test desktop and mobile states: variant badge ("Retry variant · Same idea, fresh wording"), resolved topics and retry queue chips, instant corrections, and keyboard/touch navigation.
- Data retention: store private retry pools in Postgres with data inventory updates, nightly backup fixtures, and account deletion erasure within 30 days.
- AI reliability: add `lesson_test_variant` golden set cases with deterministic checks and judge evaluation, passing `mise run engine:eval`.

## Impact
Proto definitions and generated Go/Rust bindings, API lessontest service and database migrations, engine validation and evals, and client test player. Spaced retries/recaps (E4-03), resume (E4-04), and next lesson generation (E4-05) remain separate.
