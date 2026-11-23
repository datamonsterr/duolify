## Why
A Learner who finishes the last published Lesson of a pinned Course copy needs to continue. The owner's snapshot stops there, so the next Lessons are generated for that Learner, at that Learner's cost, through the existing gated pipeline (E4-05), and stored only in that Learner's copy.

## What Changes
- New owner-of-copy routes (actor from session): `GET /v1/courses/{id}/missing-lessons` (ready/total, next missing Lesson, gate state), `POST /v1/courses/{id}/missing-lessons/quote` (a fresh `lesson_content` quote with narration ceiling at the active Pricing config version) and `POST /v1/courses/{id}/missing-lessons/generate` (explicit consent plus the shown quote).
- Generation reuses `lessontest` Prepare (extracted from the existing handler, behaviour unchanged), the River `lessons` queue and the credits reserve, settle and release components. No second generator, no new proto.
- Refusals before any Engine call: `consent_required`, `quote_stale` (expired, not this Learner's, or a Pricing config version that is no longer active), `previous_lesson_not_passed`, `insufficient_credits`, `copy_not_found`.
- Failure releases the undelivered components, keeps all earlier Lessons and allows a retry.
- New table `copy_generation_consents` records consent (quote, price version, time) per generation. Snapshot, publication hash and other copies are never written.
- Client: "n of m lessons ready" plus a quote sheet (price version, content and audio credits, balance, consent checkbox, Generate, Cancel) on the path of a copy; desktop panel, mobile sheet; loading, offline, insufficient credits, stale quote and failure states with Retry.
- Analytics: `published_lesson_generated_on_demand` (course_id, lesson_id, version); `lesson_generated` and `$ai_generation` already flow through the reused pipeline.
- Engine: additive bounded `sharing::check_missing_lesson` envelope check plus a recorded `published_copy_lesson` golden task (schema check, deadline, bounded fallback within quote and USD caps). No proto change.

## Out of scope
Republish and update flow (E15-05), invites, marketplace. ADR 0012 (E15-12): the learner pays for missing Lessons, no flow-back to the owner, no forked updates.
