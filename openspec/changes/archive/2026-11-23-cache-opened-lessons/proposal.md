## Why
Opened lessons currently require network reads on every slide and cannot restart offline.

## What Changes
- Publish SHA-256 checksums for immutable HTML and assets in authorized manifests.
- Store complete verified lesson bundles atomically in a bounded account-scoped Cache API cache; preload slides and retain valid versions after failed replacement.
- Cache only the public app shell through a service worker; bootstrap an existing credential-bound account offline and clear private bundles on logout/account change.
- Provide accessible offline, download, loading and recovery states and privacy-safe allowlisted analytics.

## Impact
- Affected specs: lesson-player, data-backup, analytics
- Affected code: player API and client, session lifecycle, public service worker
- No new learner tables, AI calls, or offline progress synchronization (E3-04).
