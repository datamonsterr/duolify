# Save raw quick notes before enrichment (E9-01)

## Why
Learners need to capture a thought during a slide or lesson immediately, even when offline or when zero credit balance prevents immediate AI enrichment (#100).

## What Changes
- Add persisted `quick_notes` table in Postgres storing raw text, authorized lesson/slide source, stable action key, and `enrichment_pending` status.
- Implement API endpoints for saving and listing quick notes with source authorization and idempotent sync retries.
- Implement client notes sheet/panel with offline device queue, distinct local vs server save state indicators, character count validation (up to 2,000 characters), and player focus/position restoration on sheet close.
- Add `quick_note_created` and `ui_sheet_opened` to the analytics allowlist and catalogue with strict privacy protection (no note text).
- Update account privacy export/deletion policy, backup fixtures, and data inventory for `quick_notes`.

## Impact
- API: `services/api/internal/notes`, `services/api/internal/httpapi`, `services/api/internal/accounts/privacy.go`, `services/api/internal/analytics/analytics.go`, `services/api/internal/backuptest/fixture.go`.
- Client: `apps/client/src/features/notes`, `apps/client/src/features/lesson-player`, `apps/client/src/lib/analytics.ts`.
- Documentation: `docs/data-inventory.md`.
- No AI behavior in this ticket.
