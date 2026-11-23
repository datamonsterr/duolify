## Snapshot
`BuildSnapshot` copies only allowlisted fields: course title/description/language, units, lesson titles/goals, slide manifest and generated Motion HTML, lesson test questions, private retry pool, narration refs (optional, excluded when `SHARING_INCLUDE_NARRATION=false`, the default per ADR 0012). Profile, notes, bookmarks, chat, answers, progress and steering instructions are never read. The scanner walks the final JSON against a closed key allowlist; any other key rejects the build (`snapshot_rejected`). Text is scanned for emails, phones and API-key patterns; findings carry only a kind and a `lesson N · slide M` location, never the matched text.

Numbering: `(course_id, number)` unique, next number under a course row lock. Hash: SHA-256 of canonical JSON. A trigger rejects UPDATE of snapshot/hash/number and DELETE except by account deletion cascade.

## Screening
API sends a bounded sample (<=12 Lessons chosen by even spread, text truncated to a 120000-byte budget) to `ScreenCourse`. Limits: input 60000 tokens, output 300, timeout 120000 ms, two total attempts (primary plus one fallback), `max_cost_nanos` from config. The API validates the response (policy version `course-v1`, attempts within the envelope, summed cost within cap, last attempt delivered); anything else stores `pending_screening`. Scanner findings with kind key/secret force at least `needs_review`; a model `block` always wins.

## Authorization
Actor comes from the session. Non-owner access returns neutral 404 `draft_unavailable`. Publishing: non-owner 404, under-age `adult_required`, owner `visibility_unavailable` (no public visibility yet). Social contract (`BlockedEitherWay`, `CanInvite`, `OnBlock`) is untouched here; E15-06 calls it.

## Analytics
`course_screening_completed` (`verdict`, `lessons_sampled`, `finding_count`, `fallback_used`) and `$ai_generation` (`feature=course_screen`). No `course_published`. Properties carry no Course or Learner text.
