# Fulfill Stripe credit packs exactly once (E7-06)

## Why
Illustrative orders cannot yet receive authenticated payment evidence. Browser returns must never mint Credits.

## What Changes
- Add an explicitly injected test-mode Checkout provider and raw-body signed webhook inbox.
- Retrieve canonical Sessions and atomically deduplicate payment intents, append purchases and queue financial analytics.
- Show pending, failed, expired and verified outcomes with saved-work recovery on desktop and mobile.
- Cover payment history in financial backups, account export/deletion and payload expiry.

## Impact
API credits, client credits, analytics catalogue, financial backup fixtures and privacy policy. No AI changes or live billing enablement; E7-05 remains required.
