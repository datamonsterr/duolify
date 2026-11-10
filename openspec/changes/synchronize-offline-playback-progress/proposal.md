# Synchronize offline playback progress once

## Why
E3-04 (#75) requires offline playback progress to be queued locally with stable action keys and synchronized exactly once on reconnect, reconciling canonically in Postgres without granting premature completion or XP.

## What changes
- Device offline queue holds non-authoritative playback progress events with stable action keys until acknowledged by the server.
- The UI visibly displays "Waiting to sync" while progress reconciliation is pending, clearing upon canonical reconciliation.
- An authorized synchronization API endpoint (`POST /v1/lessons/{lesson}/sync`) records unique events, merges out-of-order progress into the authoritative cursor, and rejects foreign/malformed payloads.
- Deep links to stable slide IDs resolve accurately without implying lesson completion; the last slide links solely to the lesson test.
- PostHog test-double analytics emits `slide_viewed(offline=true)` without sending slide or learner text.
- Database migration adds `lesson_playback_sync_events`, extends cursor monotonicity, and updates privacy export/deletion and backup verification.

## Impact
- Affected areas: `apps/client`, `services/api`.
- Specs: `lesson-player`, `data-backup`, `analytics`.
- No new AI behavior or eval changes.
