## Decisions
- **Copy on enroll.** The copy is a normal `courses` row owned by the learner (`creation_id` becomes nullable) with its own units, lesson stubs, slides, questions and retry pools copied from the pinned draft snapshot. The existing player, Lesson test, XP, notes and chat code therefore work unchanged and stay learner-specific. Deleting the source Course or the owner cannot remove a copy.
- **Pin.** `course_copies(copy_course_id PK, learner_id, source_course_id, draft_id, version, content_hash, created_at)`, `UNIQUE(learner_id, source_course_id)`. `source_course_id`/`draft_id` are `ON DELETE SET NULL`; version and hash stay for restore checks.
- **Idempotency.** Enrollment runs in one transaction behind `pg_advisory_xact_lock(hash(learner, source))`; a second call returns the existing copy with `created=false` and emits no event.
- **Zero credits.** Reuse never touches `pricing`/`credits`. Missing Lessons keep the existing quote/reserve/settle flow (E15-04).
- **Unlock.** Stub 0 is `current` when the snapshot has it, otherwise `ready_to_generate`. After a pass, the next stub becomes `current` when it already has a generated Lesson, else `ready_to_generate`. Gate checks are unchanged.
- **Safety.** Block check uses `social.BlockedEitherWay`. The owner opening their own link gets their own Course back (no copy). Errors are neutral: `link_not_found`, `rate_limited`.
- **Snapshot gaps.** Snapshot slides carry no duration; the copy uses config `SHARING_COPY_SLIDE_MS` (default 8000). Snapshots exclude narration audio (ADR 0012).
- **Response** `{course_id, created, version, total_lessons, ready_lessons, needs_generation_lessons}`; no token, no owner identity beyond the preview.
