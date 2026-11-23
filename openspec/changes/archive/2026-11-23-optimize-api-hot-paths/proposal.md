# Optimize API hot paths

The 2 vCPU API host serves three paths far more often than their data changes: the public pricing catalogue, the learner's course list and the course-creation event stream. This change removes the repeated work without changing what a learner sees.

- `GET /v1/pricing` is served from an in-memory snapshot that expires after 5 s (and is replaced at once when this process activates a version), so a warm request runs no SQL. Staleness across replicas is bounded by that TTL.
- Public, non-personal GETs get an explicit cache policy with ETags; every per-learner response stays `private`, never `public`.
- `GET /v1/courses` returns a summary per course (card fields and progress counts) instead of every unit and lesson; the tree is fetched only for the opened course through `GET /v1/courses/{id}`. The list supports `ETag` and `If-None-Match` (304).
- The web client gets a configured query client: 30 s staleness, no refetch on window focus, and no retries for 4xx, sign-in, offline or failed-fetch errors. Offline behaviour is unchanged.
- The course-creation event stream polls with backoff (200 ms, doubling to 3 s, reset on every state change) instead of every 200 ms. Heartbeats and terminal events are unchanged.

No AI behaviour changes, so no new evals or fallbacks. Stored data is unchanged: no migration and no change to backup or restore. No analytics event changes. No client has shipped, so the list response changes shape without a compatibility period; the web client also accepts the old full-tree shape.

Out of scope: an auth or session cache, the lesson-generation stream (`internal/lessontest`), the background activator interval, and `::text` cast rewrites outside the list query.
