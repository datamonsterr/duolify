# Rename and find learners with privacy controls (E6-02)

## Why
Learners need to rename without enabling impersonation, find each other through bounded search, and control how findable they are (#91). Builds on E6-01 usernames and holds.

## What Changes
- `PATCH /v1/social/username` renames: 30-day cooldown counted from the last change, old name held 30 days (E6-01 `username_holds` trigger), previous owner may reclaim a held name, in-flight references stay learner-ID based.
- `GET /v1/social/search?q=` exact or prefix search (min 3 characters, max 20 results, 30/min per learner) returning only username, display name and avatar. Nondiscoverable learners appear only for an exact match and only when `request_policy` is `everyone`. A single named seam (`blockedPairClause`) is where E6-04 adds the bilateral block filter.
- `GET`/`PATCH /v1/social/settings` for `discoverable`, `request_policy`, `show_streak_to_friends`.
- Per-learner Postgres rate limits (`social_rate_limits`, stateless API) for searches and for username claim/rename attempts (closes name probing left open by E6-01). Limits live in `username_config`.
- Client: rename form, Add-a-friend search sheet (Friends) and Privacy & social settings (Settings), desktop and mobile.
- Analytics: `username_changed`, `username_searched`, `privacy_setting_changed`.
- Data: `social_rate_limits` classified, fixture covers rename hold and privacy, inventory updated.

## Impact
- API: `internal/social`, `internal/analytics`, `internal/accounts/privacy.go`, `internal/backuptest`, `migrations`, e2e API server mounts social.
- Client: `features/social`, Settings. No AI behavior; evaluation and fallback gates unchanged.
