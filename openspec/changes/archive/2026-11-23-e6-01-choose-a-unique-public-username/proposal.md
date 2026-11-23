# Choose a unique public username (E6-01)

## Why
Friends, invites and search need a unique public handle. This is the first social tracer: username onboarding and a minimal public identity card (#90).

## What Changes
- Add `learner_usernames` (canonical NFKC+lowercase unique index, display case kept) and `username_holds` (released names reserved 30 days; reusable by the E6-02 rename hold) in Postgres, plus `username_config` for the reserved and profanity lists.
- Add `services/api/internal/social`: claim endpoint with exactly-one-winner concurrency, neutral rejection codes, and a public projection exposing only username, display name, avatar and privacy-permitted streak status.
- Add the client `features/social` claim form and identity card to Friends and Settings (desktop and mobile).
- Add `username_set` and `username_rejected` to the analytics allowlist and catalogue (reason codes only, no text).
- Extend privacy export/deletion policy, backup fixtures and `docs/data-inventory.md`.

## Impact
- API: `internal/social`, `internal/httpapi`, `internal/accounts/privacy.go`, `internal/analytics`, `internal/backuptest`, `migrations`.
- Client: `apps/client/src/features/social`, `App.tsx`, Settings.
- No AI behavior; evaluation and fallback gates unchanged. No proto change.
- Out of scope: rename (E6-02), search, privacy settings UI, friend requests (E6-03), blocking (E6-04).
