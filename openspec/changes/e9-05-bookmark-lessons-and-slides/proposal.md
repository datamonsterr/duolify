# Bookmark lessons and exact slides (E9-05)

## Why
Learners need to save a pointer to a specific lesson or exact slide and return to it later quickly without AI processing. Bookmarks must support offline creation and removal with idempotent sync, course filtering, authorized deep links to slides, and clear unavailability states for removed targets (#104).

## What Changes
- Add persisted `bookmarks` table in Postgres storing learner-target pointers (lesson or stable slide index), stable action key, and course/lesson metadata.
- Implement API endpoints for creating, listing, and removing bookmarks with target uniqueness, authorization, and course filtering.
- Implement client bookmarks view with course filter chips, cached slide opening, online recovery for uncached slides, unavailable state labels, and player one-tap bookmark toggle with toast.
- Add `bookmark_added`, `bookmark_removed`, and `bookmark_opened` to the analytics allowlist and catalogue without leaking private text.
- Update account privacy export/deletion policy, backup fixtures, and data inventory for `bookmarks`.

## Impact
- API: `services/api/internal/notes`, `services/api/internal/httpapi`, `services/api/internal/accounts/privacy.go`, `services/api/internal/analytics/analytics.go`, `services/api/internal/backuptest/fixture.go`.
- Client: `apps/client/src/features/notes`, `apps/client/src/features/lesson-player`, `apps/client/src/lib/analytics.ts`, `apps/client/src/App.tsx`.
- Documentation: `docs/data-inventory.md`.
- No AI behavior in this ticket.
