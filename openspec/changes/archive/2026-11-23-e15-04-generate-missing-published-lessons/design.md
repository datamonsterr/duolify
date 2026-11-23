## Decisions
- **No proto change.** The copy is a normal learner Course, so `GenerateLesson` is called exactly as for private Courses. Engine work is a pure additive check and a recorded eval task.
- **Reuse.** `lessontest.Service.Prepare(ctx, owner, lessonID, quoteID)` holds the old `handlePrepare` logic (stub eligibility, predecessor pass, one in-flight generation per Lesson, Cast pin, River insert). The HTTP handler is now a thin mapper.
- **Gate order in `generate`.** actor owns a copy, consent true, quote is this Learner's `lesson_content` quote, unexpired and at the active Pricing config version, next missing Lesson equals `lesson_id`, predecessor passed, wallet `available` covers content plus audio ceilings, then accept the quote, record consent and Prepare. The worker still reserves authoritatively; a failed reservation fails the job before the Engine and releases.
- **Quote.** `lesson_content` with the configured narration characters bound and the first allowed voice. The panel shows `pricing_version`, content and audio credits and expiry. Narration audio is regenerated per Learner (ADR 0012).
- **Ownership.** The reservation, settlement and ledger rows belong to the generating Learner. Nothing is written for the owner (no flow-back).
- **Copy-local.** Generated rows use the existing learner tables keyed by the copy's stub. `course_copies.content_hash` and the snapshot stay immutable.
- **Analytics.** `published_lesson_generated_on_demand` is emitted by the worker after a committed settle when the Course has a `course_copies` row.
- **Consent record.** `copy_generation_consents(generation_id PK, learner_id, copy_course_id, lesson_id, quote_id, pricing_version, consented_at)`; cascades on learner deletion; in the nightly dump.
- **Fallback bounds.** Existing quote bounds apply: `max_attempts`, `timeout_ms`, `max_cost_nanos`, one evaluated fallback model.
