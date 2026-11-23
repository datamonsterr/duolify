## Decisions
- **Live version** is the latest `pass` draft (as in E15-01..03/07). Republish never changes visibility; a non-pass version is stored but ignored by preview, enrollment and listing.
- **Cap** counts `publication_drafts` rows with `republish=true` in the last 24 hours, inside the owner row lock.
- **Lesson key**: first 32 hex of `sha256('lesson-key:'||stub_id)`, written to `course_lesson_stubs.lesson_key` when a draft is built. Legacy snapshots without `key` get `legacy-<unit>-<lesson>` at read time.
- **Update** runs in one transaction under `FOR UPDATE` on the copy pin; `lock_version` must match and is incremented. Positions are rewritten through an offset to satisfy `UNIQUE(unit_id, position)`; archived stubs move to a hidden unit (`course_units.archived_at`) at position 1,000,000 so unlock queries never reach them. Status after update: done stays done; the first not-done lesson opens (`current` when it has content, else `ready_to_generate`); the rest are `locked`.
- **Keep vs replace**: a matched stub keeps its content when it is done, learner-generated, or has a test session; otherwise reused content is replaced by the new version's.
- **Neutral errors**: unknown, foreign, forked or already-current copy and stale lock are all `update_unavailable` 409/404 without detail.
- **Changelog**: owner text (max 500 chars, config) plus counts computed from the two snapshots (added, removed, retitled) at read time.
- **Retention**: a prefix `lessons/published/<draft>/` is retained while any copy pins the draft or any lesson manifest of a copy points into it.

## API contract
- `POST /v1/courses/{id}/republish` `{changelog?}` → 201 draft (+`live`); 404 `draft_unavailable`, 403 `adult_required`, 409 `not_published`, 429 `republish_limit` (+`Retry-After`).
- `GET /v1/courses/{id}/versions` → `{versions:[{number,verdict,live,lesson_count,changelog,created_at}],republishes_remaining,limit}`.
- `GET /v1/courses/{copy}/update` → `{update_available,forked,current_version,latest_version,lock_version,changelog:[{version,text,added,removed,retitled}],completed_lessons,archived_count}`.
- `POST /v1/courses/{copy}/update` `{lock_version,to_version}` → 200 `{version,lock_version,kept,added,archived}`; stale → 409 `update_unavailable`.
- `GET /v1/courses/{copy}/archived-lessons` → `{lessons:[{title,archived_at,version,completed}]}`.
