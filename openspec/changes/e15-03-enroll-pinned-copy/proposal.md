## Why
A Learner who opens a live share link needs one Course copy of their own, pinned to the published version, whose already generated Lessons cost nothing. Progress, Lesson tests, notes, chat and XP stay learner-specific; the creator sees counts only.

## What Changes
- `POST /v1/public/shares/{token}/enroll` (signed-in actor only) creates ONE learner-owned copy per learner and source Course (unique, idempotent) pinned to the latest passing publication draft. Unknown, revoked, private, deleted or block-hidden links get the neutral `link_not_found`. Existing copies keep working and are listed with `GET /v1/courses`.
- New tables `course_copies` (pin: source course, draft, version, content hash, learner) and a `reused_from_snapshot` flag on lesson stubs.
- Snapshot lessons are copied into the learner's own Lesson rows at zero Credits: no quote, reservation or charge. Answer keys stay server-side and never appear in manifests.
- Unlock order is unchanged: a reused Lesson becomes `current` only after the predecessor test pass. A Lesson missing from the snapshot stays `ready_to_generate` (the E15-04 hook; the copy reports `ready_lessons` and `needs_generation_lessons`).
- Per-account daily enrollment limit (config, default 30) returns `rate_limited`.
- The creator-visible enrollment count is the number of copies (aggregate only).
- Client: enroll panel on the link preview (sign in, enroll, already enrolled, refused, offline) and ready/needs-generation markers on the path.
- Analytics: `course_enrolled_published`, `published_lesson_reused`.

## Out of scope
Generation of missing Lessons (E15-04), updates, invites, marketplace, reviews (E15-05+). Interim policy ADR 0012 (E15-12): the learner pays for missing Lessons, no flow-back, no forked updates.
