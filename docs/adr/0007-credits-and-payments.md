# ADR 0007: Credits ledger, daily free credits, subscriptions, no credit rewards

- Status: Accepted (owner approved E12 pricing, 2026-11-08)
- Date: 2026-11-08

## Decision

- Credits are an append-only ledger in Postgres (`credit_entries`: learner, amount, kind `daily_grant|purchase|subscription_grant|spend|reserve|release|refund|expire`, reference). Balance is a derived, cached value.
- **Daily free credits**: every learner gets a daily grant sized to cover 3 quick notes and 5 to 8 tutor chat messages. The grant expires at the end of the learner's local day and is spent first.
- AI calls: reserve an estimate, settle on actual usage, release the rest. Prices per feature and model live in config.
- **No credit rewards for effort.** Effort is rewarded with streak freezes (applied automatically), XP and badges, never credits.
- Paid credits: one-time credit packs and subscriptions (monthly/yearly) with a monthly credit allowance and benefits. Prices, pack sizes, tiers and the daily free amount come from E12 and live in the **pricing config**: versioned, validated at load, changeable without a deploy, every ledger entry records the price version it used.
- Payments via Stripe Checkout (web flow opened from the app). Mobile store billing rules are part of E12.

## Consequences

- Full audit trail; refunds and expiries are new entries, never edits.
- The `credit_entries.kind` values in the bootstrap migration change in E12 (`reward` is dropped).
