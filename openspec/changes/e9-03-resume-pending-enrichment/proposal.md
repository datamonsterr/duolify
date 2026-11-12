# Resume pending enrichment when credits arrive (E9-03)

## Why
When learners jot quick notes without spendable credits (or when providers temporarily fail), notes are safely preserved in `enrichment_pending` status. However, learners should not have to manually track down and re-trigger enrichment after receiving their next Daily free credits or purchasing a credit top-up. Enrichment should resume automatically, oldest notes first, until eligible credits are exhausted, without double enrichment or charging stale note revisions (#102).

## What Changes
- Automatically wake pending quick note enrichments when credits arrive (daily free grants via `EnsureDailyGrant` or credit outbox event processing, and one-time pack or subscription credit fulfillments).
- Order pending note processing strictly oldest first (`created_at ASC`) per learner.
- Utilize Postgres queue locks (`FOR UPDATE SKIP LOCKED`) and note status transitions to eliminate concurrent race conditions and double enrichments.
- Track note raw text versioning (`raw_version` on `quick_notes`), ensuring stale raw-text revisions cannot be overwritten by old enrichments and retries never charge a delivered version twice.
- Retain raw notes and release reserved credits if budget is exhausted or engine provider fails.
- Client UI surfaces pending and resumed enrichment status matching canvases `S21-Notes.dc.html` and `S09-Sheets.dc.html`, displaying upsell recovery options (`credits_upsell_shown`) and manual retry when appropriate.
- Golden set eval test cases in `evals/fixtures/` covering stale-version, credit exhaustion, and retry scenarios.

## Impact
- API: `services/api/internal/notes`, `services/api/internal/credits`, `services/api/migrations/`, `services/api/queries/quick_notes.sql`.
- Client: `apps/client/src/features/notes/`, `apps/client/e2e/notes-e9-03.spec.ts`.
- Engine: `engine/crates/engine-core/src/notes/tests.rs`, `evals/fixtures/golden.sample.jsonl`.
- Docs: `docs/data-inventory.md`.
