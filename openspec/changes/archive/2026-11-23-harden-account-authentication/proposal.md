## Why

Review #163 found proxy-wide throttling, active-code replacement, refresh races, permissive runtime defaults and unnecessary browser credential persistence.

## What Changes

- Resolve rate-limit identities using a configured trusted proxy hop count, defaulting to no trust.
- Reuse active email codes, separate caller and delivery budgets, and enforce resend cooldowns.
- Require explicit development configuration for local mail, fixture providers and localhost CORS.
- Return the same refresh successor during a bounded grace period, with family revocation for later replay.
- Separate authentication keys, validate provider claims and nonce, bound setup drafts and deny unknown routes by default.
- Keep browser credentials in memory and restrict the Tauri script policy.

## Impact

Accounts API, API startup, client credential/OAuth boundaries and encrypted backup fixtures change. Existing codes and OAuth attempts are invalidated on migration. JWT key separation invalidates existing access JWTs; active refresh secrets can recover sessions. No new analytics events or learner text are collected. AI generation behavior is unchanged; oversized setup drafts are rejected before engine calls. Authentication tables remain in encrypted backups, and account deletion removes email-linked codes and throttles.
