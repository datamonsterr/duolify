# Create the confirmed edited outline

## Why
E2-02 completes Outline → Create. Older sample/read-only wording must not permit generation to discard manual edits or applied chat changes.

## What Changes
- Bind final quote and consent to the confirmed draft revision and hash.
- Queue an idempotent outline-only River job; validate exact authored structure before atomic enrollment.
- Stream durable stages and restore the enrolled path; release undelivered reservations on failure.
- Cover learner data export/deletion, nightly backups and offline model reliability.

## Impact
API, engine, private proto, client, privacy inventory and backup fixtures. No lesson generation or course switching/deletion UI.
