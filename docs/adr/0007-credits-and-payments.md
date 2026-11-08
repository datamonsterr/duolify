# ADR 0007: Credits ledger, Stripe for top-ups, effort rewards

- Status: Proposed (confirm)
- Date: 2026-11-08

## Decision

- Credits are an append-only ledger in Postgres (`credit_entries`: learner, amount, kind `purchase|reward|spend|reserve|release|refund`, reference). Balance is a derived, cached value.
- AI calls: reserve an estimate, settle on actual usage, release the rest. Prices per model live in config.
- Top-ups via Stripe Checkout (web flow opened from the app). Mobile store billing rules are a later decision.
- Rewards for effort: streak milestones, perfect lessons and friend-streak milestones grant reward credits and streak freezes.

## Consequences

- Full audit trail; refunds are new entries, never edits.
- Stripe integration is a later OpenSpec change; bootstrap only defines the ledger shape in specs.
