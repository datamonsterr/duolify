## Why
An owner who improves a shared Course needs to publish a new immutable version, and learners who copied it must choose whether to move, without losing completed work. E15-03 pins copies to one version; this change adds the version lineage, the explicit update and the stable lesson key that makes a safe update possible.

## What Changes
- `POST /v1/courses/{id}/republish` (owner): builds version n+1 through the E15-01 draft and screening path with an optional changelog. Only a `pass` verdict becomes live (the previous live version, listing, link and enrollments keep serving until then). At most 3 republishes per Course per rolling day (`SHARING_MAX_REPUBLISHES_PER_DAY`).
- `GET /v1/courses/{id}/versions` (owner): version list with verdict, live flag, changelog and the remaining daily budget.
- Stable lesson key: `course_lesson_stubs.lesson_key`, derived deterministically from the owner stub id, persisted at the first draft and carried in every snapshot lesson as `key`. Retitles, reorders and unit moves do not change it. Copies store the key of each lesson they hold.
- Learner routes `GET /v1/courses/{copyId}/update` (availability, changelog, progress summary, `lock_version`), `POST /v1/courses/{copyId}/update` (explicit update guarded by `course_copies.lock_version`) and `GET /v1/courses/{copyId}/archived-lessons` (history).
- Update maps lessons by key: matched lessons keep their stub (tests, progress, notes, learner-generated E15-04 content) and are retitled/reordered; untouched reused lessons take the new content; lessons new upstream are added; lessons removed upstream move to an archived unit and stay in the history. Forked copies (`course_copies.forked_at`) never see or apply updates (ADR 0012).
- A stale `lock_version` or version is rejected with a neutral `update_unavailable` 409. Pin and update are serialized by the row lock plus the version column.
- Object references: objects of every published version still pinned by a copy or referenced by a copy lesson manifest are retained; `RetainedObjectPrefixes` is the single source for any later purge.
- Marketplace listing and share preview always follow the latest passing version after a republish.
- Analytics `course_republished`, `course_update_applied` (UUIDs, enums, counts).
- Data: `publication_drafts.changelog/republish`, `course_copies.lock_version/forked_at`, `course_lesson_stubs.lesson_key/archived`, `course_units.archived_at`; privacy export/erasure, data inventory, backup fixture, anonymize classification.
- Client: owner "Publish new version" with changelog on the Share screen; learner "Update available" sheet per `docs/design/canvas/E15-Share.dc.html` and `E15-Market.dc.html`, desktop and mobile.

## Out of scope
Forked-copy updates, merging owner and learner edits, automatic updates, purge of unpinned objects (E15-11).
