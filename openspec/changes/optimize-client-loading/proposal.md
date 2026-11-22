## Why
The web and desktop client shipped as one eager bundle (about 383 kB gzip of JavaScript before the first screen), initialised posthog-js before first paint, precached every built asset on install, and refetched course data on every visit because nothing invalidated it.

## What Changes
- Split every route that is not the first screen (sign-in and the Learn home stay eager) into its own chunk, with a router pending state and a Retry error state when a chunk fails to load. Hovering or focusing an in-app link preloads its route.
- Load and initialise posthog-js after first paint, only once consent is resolved to opted in. Events captured before it is ready are queued and delivered in order; opting out drops the queue and cancels the load.
- The service worker precaches only the first-screen shell on install. Route chunks are cached on first use (cache-first, hashed files). Opening a lesson or downloading a course also caches the lesson-player, lesson-test and course-list chunks, and every later worker version keeps them cached.
- Lesson progress, a passed lesson test, and a course created, deleted, switched or re-levelled invalidate the course-tree and course-list queries; the `staleTime: 0` workarounds are removed.
- The Learn home starts the course-tree fetch as soon as the course list names the active course.
- A CI bundle budget (`mise run client:bundle-budget`) fails when first-load JavaScript or CSS grows past the recorded limit.

## Impact
- Affected specs: lesson-player, analytics
- Affected code: apps/client (router, service worker, analytics boundary, course queries), .mise/tasks/client.toml, CI client job
- No API contract, schema, AI behaviour or backup change. Analytics settings (autocapture off, no automatic pageviews) are unchanged.
- Data backup: unchanged; no new stored data (the service-worker caches hold public build assets only).
- Model testing and reliability: not applicable, no AI is involved.
