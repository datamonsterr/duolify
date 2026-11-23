## Decisions
Use Cache API in the host, never iframe storage. One JSON response contains the manifest, verified raw HTML and verified assets; Cache.put publishes the whole response only after all bytes and limits pass. A failed fetch/hash/quota write cannot replace a valid bundle. Keep at most five lessons and 40 MiB per account, each bundle at most 8 MiB. Serialize publications and invalidate work on session changes. Verify bundles on reads, fail closed on missing/corrupt entries, and never fall back after authorization errors. Use data URLs for verified assets with a network-denying cached-document CSP and the existing opaque sandbox. Only static public shell resources enter the service worker cache; API and private assets never do.

A minimal verified identity (UUID and opt-out) is bound to the stored refresh credential by SHA-256 for offline bootstrap. No tokens or learner text are persisted there. Credential deletion/account switches invalidate identity and clear bundles. Offline position upload remains outside this change.

## Documentation checked
MDN Cache/Service Worker/SubtleCrypto references and TanStack Query prefetching guide, 2026-11-10.

## Data and backup
Server object versions remain authoritative under the existing 14-day backup and tracked restore policy. Device cache and minimal identity are disposable, excluded from backups/export, removed on logout/deletion. No new Postgres data or model behavior.
