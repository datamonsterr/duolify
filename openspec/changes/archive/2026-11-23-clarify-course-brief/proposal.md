# Clarify a course brief (E2-06)

## Why
Learners need to shape and confirm their intent before the separately quoted paid preview.

## What Changes
- Add stateless CourseBriefChat gRPC and authenticated HTTP chat, config and confirmation/read endpoints.
- Reuse guided chat, editable summary and responsive sheet with a session-only draft and form fallback.
- Persist immutable learner-owned confirmed revisions; never generate previews, reserve Credits or create Courses here.
- Add proposed course_brief_chat=0 to the unapproved development Pricing config, platform usage and throttles.
- Add privacy-safe analytics, offline golden fixtures and backup/export/deletion coverage.

## Impact
Touches proto, engine, API, client, eval fixtures and personal-data documentation. E2-01 and E7 remain outside scope.
