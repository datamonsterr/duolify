## Decisions
- Identity: `sha256(len-prefixed normalized text, voice id, model id, settings fingerprint)`. `text_hash` (normalized text only) links retained settings versions so a changed voice setting makes a new version and keeps the old one.
- Dedup uniqueness: `narration_assets.content_hash` primary key plus a per-hash Postgres advisory lock (taken in sorted order on one connection, so concurrent runs cannot deadlock). A waiter re-reads the asset and hits the cache.
- Aggregate price: one `narration` quote with the sum of newly synthesized characters; `credits.CalculateSettlement` already sums delivered characters and rounds up once (50/1,440 Credits per character from Pricing config). Undelivered slides add no usage; if nothing was delivered the reservation settles to zero.
- References: a trigger on `slide_narrations` adjusts `ref_count` for `ready` rows only (insert, status change, hash change, delete incl. cascades). The last reference sets `unreferenced_at`; `PurgeUnreferenced` deletes rows and objects after retention and re-checks references in the delete.
- Recovery order for a missing object: (1) restore the latest bucket version (remove the delete marker), (2) repoint all slides to a retained prior settings version of the same text whose objects exist, (3) mark the asset `missing`, park slides as `pending/object_missing` and record one open `narration_regenerations` row per hash. The operator job regenerates only the identical hash (else fails with a fixed code) with platform-funded usage; no reservation exists, so the Learner is never charged.
- A new request for a `missing` asset parks its slides the same way instead of synthesizing and charging.
- Manifest statuses stay `pending|ready|failed|disabled`; recovery uses `pending` plus the status endpoints' `state` (`ok|restored|regenerating|pending|failed`).
- Analytics reuse `narration_cache_hit`, `narration_generated`, `narration_failed` (already in the catalogue); only ids, counts and fixed codes.

## Risks
- Aggregate reservation limits one lesson run to the quote's narration character bound; larger lessons fail closed with `quote_unavailable` and need a fresh quote (E12 bounds).
- Cache lookup trusts `ready` assets; a lost object is detected on Play/Restore, not on every request.
