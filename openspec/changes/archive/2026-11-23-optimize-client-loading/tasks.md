## 1. Specification and tests
- [x] 1.1 Baseline the build (entry, total, chunk list) and record it in the PR.
- [x] 1.2 Unit tests: analytics queue and consent, chunk-load retry, course query invalidation, offline manifest.
- [x] 1.3 Playwright: first load skips player/marketplace chunks, lesson loads its chunk, opened lesson caches its route chunks and replays offline.
## 2. Implementation
- [x] 2.1 Route-level code splitting with router pending and Retry error states; hover/focus preload.
- [x] 2.2 Lazy, consent-gated posthog-js behind an ordered queue.
- [x] 2.3 Shell-only precache, runtime caching, warm lesson chunks on open and download.
- [x] 2.4 Invalidate course queries on progress, test pass, create, delete, switch; remove `staleTime: 0`.
- [x] 2.5 Start the course-tree fetch when the list names the active course.
- [x] 2.6 `client:bundle-budget` task wired into the client CI job.
