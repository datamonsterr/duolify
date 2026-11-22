# Fix core-loop defects found by the full-stack flow suite

## Why
The full-stack flow suite (#283) found that a new learner cannot get through the core loop in
the real stack: course preview returns 502 because the active pricing version quotes a route the
engine no longer serves, in-app links reload the page and drop the in-memory web session, the
Learn home renders a fixture course for a learner with no courses, and the streak, league,
daily-goal and analytics panels call authenticated endpoints without the access token.

## What Changes
- Same-app links and redirects navigate through the router (`lib/navigation`: `AppLink`,
  `navigateTo`) instead of `<a href>` page loads and `location.assign`.
- The shell has no default active course; a learner without courses sees the empty state.
- Gamification reads and writes use `session.request` (access token, one refresh on 401).
- API and worker log an error at startup for each quoted action whose active pricing route the
  engine will refuse (provider mismatch with the engine's Health providers), naming the action,
  pricing version and both routes. No behaviour change.
- A CI test fails when `engine/config/models.toml` routes are not covered by the newest
  Hy4-routing quote policy proposal.
- The e2e overlay stops forcing `LESSON_TEST_TRACER_ENABLED=false` (production default is on).

Out of scope: approving or activating any pricing version (owner-gated), persistent web
sessions (after the Clerk PR #276), engine changes.

## Impact
apps/client, services/api (startup signal, test), infra/e2e overlay. No proto or schema change.
Data backup: no new data. Analytics: no new events. AI reliability: the route signal and the
coverage test make quote/engine route drift visible before learners hit it.
