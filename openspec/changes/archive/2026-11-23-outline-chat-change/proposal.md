# E2-08: Review paid outline-chat changes before applying

## Why
Learners need to request a change to their edited sample outline without losing manual edits or consenting to course creation.

## What Changes
- Add immutable, learner-owned typed proposals bound to draft revision and accepted versioned quote.
- Reuse quote and reservation execution; charge only a validated delivered proposal, including later Discard. Apply, Discard and Undo are free and idempotent.
- Add the matching engine RPC and docked desktop/mobile step diff UI using shared design components.
- Add unapproved development Pricing key `outline_chat_change`; production remains gated by E12-09/E12-10 and live evaluation promotion.

## Impact
API, engine, proto, client, analytics, privacy, backup fixtures and offline golden evaluations. E16 replans and E2-02 creation are excluded.
