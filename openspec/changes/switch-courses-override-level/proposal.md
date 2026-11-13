# Switch courses and override course level

## Why
E2-03 replaces scaffolded static course endpoints and fixed astronomy course selection with authorized enrollment reads and persistent last-active selection. Learners can set or reset a per-course learning level that feeds AI context without altering the global profile, and navigate enrolled courses with neutral foreign-access protection.

## What Changes
- Expose authorized `/v1/courses` enrollment list and persistent active course selection surviving client restarts.
- Ensure foreign course access is neutral (404 / unavailable) without leaking whether an un-enrolled course exists.
- Implement per-course level override and reset-to-global controls and endpoints without modifying global profile values.
- Build an effective-profile context builder projecting global context overridden by course-level settings for future engine calls.
- Render confirmed edited outline titles, ordered lesson goals and current badges on desktop and mobile matching S03, S08 and S23 canvases.
- Emit allowlisted analytics (`learner_profile_updated`, `ui_navigation_selected`) without private text, honoring opt-out.
- Include level overrides and enrollment preferences in nightly backups, data inventory, export and account erasure.

## Impact
API courses and accounts services, database schema migrations, client courses, navigation shell, profile settings and backup fixtures.
