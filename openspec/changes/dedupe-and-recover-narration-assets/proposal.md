# Deduplicate and recover narration assets (E14-02)

## Why
E14-01 proved synthesis with a per-slide reservation. Identical narration must never be paid or synthesized twice, failed audio must retry without a second delivered-artifact charge, and a lost object must be recoverable without charging the Learner.

## What Changes
- Normalize narration text (NFC, collapsed whitespace) and compute the content hash from normalized text, voice, model and the engine's resolved settings fingerprint before any provider call.
- Add `narration_assets` (hash, text hash, keys, counts, `ready|missing`, trigger-maintained `ref_count`, `unreferenced_at`) and `narration_regenerations` (tracked operator regeneration). A lesson now has one River job, one `narration` quote and one aggregate settlement: delivered characters are summed and rounded up once.
- Serialize identical hashes with advisory locks taken in hash order; existing ready hash is a `narration_cache_hit` at zero charge. Slides sharing a hash in one lesson reuse one object.
- `POST …/narration/retry` re-queues a failed slide under the same hash; `POST …/narration/recover` restores a missing object from the latest retained bucket version, then from a retained prior settings version of the same text, else queues a tracked operator regeneration. None reserve or charge Credits.
- Assets are deleted only after the last reference ends plus `NARRATION_RETENTION_DAYS` (default 30).
- Client: `Retry narration` and `Restore narration` recovery actions, "being restored" status, explicit Play after restore (never autoplay).
- Data: inventory, privacy classification, backup restore checks. Golden-set `lesson_narration` cache, retry and alignment-failure rows with recorded fixture `evals/fixtures/narration.dedupe.json`.

## Impact
services/api (narration, player routes, playerfixture, migrations, backuptest, accounts privacy), engine tests and eval fixtures, apps/client player, docs/data-inventory.md, `.env.example`.
