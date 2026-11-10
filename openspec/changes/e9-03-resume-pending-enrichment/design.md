# Design: Resume pending enrichment when credits arrive (E9-03)

## Context
E9-01 captured raw notes; E9-02 implemented grounded AI enrichment and left zero-credit notes in `enrichment_pending`. E9-03 completes the lifecycle by automatically resuming pending enrichments as soon as spendable credits become available through daily resets or purchases.

## Architecture and Data Model

### 1. Database Schema and Versioning
- Add `raw_version integer NOT NULL DEFAULT 1` to `quick_notes`.
- When note raw text is modified, `raw_version` increments (`raw_version = raw_version + 1`) and `status` resets to `enrichment_pending` (if re-enrichment is desired).
- Add partial index `quick_notes_pending_enrichment ON quick_notes(learner_id, created_at ASC) WHERE status = 'enrichment_pending'` to ensure fast ordered lookups.
- `quick_note_enrichments.version` records the note's `raw_version` at delivery time.

### 2. Queue Locking and Concurrency Control
- Pending enrichments are selected with `SELECT id, raw_text, status, raw_version FROM quick_notes WHERE learner_id = $1 AND status = 'enrichment_pending' ORDER BY created_at ASC FOR UPDATE SKIP LOCKED`.
- If an enrichment job encounters a note whose current `raw_version` differs from the job's `expected_version`, the stale job safely terminates without writing or charging.
- If a note is already in `enriched` status with `enrichment.version == note.raw_version`, retries do not charge or re-enrich.

### 3. Credit Arrival Wakeup Trigger
- Whenever credits arrive:
  - Daily grant issuance in `credits.Service.EnsureDailyGrant`.
  - Credit outbox dispatch of `daily_credits_granted`, `credits_purchased`, or `subscription_grant_issued`.
  - Stripe pack payment fulfillment in `payments.go`.
- Wakes pending note enrichments for the recipient learner.
- Processing proceeds sequentially oldest first until eligible credits/budget is exhausted (`credits.ErrInsufficient`).
- Notes that cannot be funded remain in `enrichment_pending`.

### 4. Client UI and Recovery
- Matches `docs/design/canvas/S21-Notes.dc.html` and `docs/design/canvas/S09-Sheets.dc.html`.
- Notes in `enrichment_pending` display status badge "Enrichment pending" and upsell banner ("Zero-credit variant: Add credits to enrich it, or wait for your daily reset").
- Emits `credits_upsell_shown` when zero-credit upsell is presented.
- Notes in `enrichment_failed` display recovery banner and "Retry enrichment · 2 credits" action.
- Real-time polling or sync refreshes note status once background enrichment completes.

### 5. AI Reliability and Evals
- Add Golden set cases for task `note_enrich`:
  - `note-enrich-stale-version`: Stale version mismatch rejects overwrite.
  - `note-enrich-exhaustion`: Provider credit ceiling limits attempt count within bounds.
  - `note-enrich-retry`: Successful retry after initial failure delivers valid summary.
## Review correction: fenced enrichment leases

Every claimed raw revision has a unique fencing token and a lease deadline. The initial lease lasts two minutes; attaching a reservation extends it through the configured provider deadline plus fifteen seconds for settlement. Manual enrichment, workers and learner-scoped resume may reclaim expired leases, including legacy enriching rows using updated_at plus two minutes. Before reserving again, recovery settles the previous undelivered reservation at zero.

All failures after claiming reset the status using the claim token, including quote, consent, credits, engine and persistence errors. Insufficient credits leave the note pending. A concurrent edit clears the old claim, and late workers cannot clear or overwrite a replacement claim. Accepted enrichment checks raw version, reservation, claim token and lease inside SettleWith. Backup fixtures preserve the lease fields with the complete note row; existing account deletion and analytics privacy policies apply. Mock-provider integration tests exercise early failures, abandoned work and late responses.
