## Why

E12 must establish whether daily free Tutor chat and Quick notes can be funded by paid generation, credit packs and subscriptions. Prices and payment entitlements need an auditable cost model before E7 implements billing.

## What Changes

- Propose a USD credit catalogue, expiring daily free allowance, non-expiring paid balances, three packs and three monthly/yearly plans, costed in [the business model](../../../docs/business/monetization.md).
- Add versioned price configuration, spend guardrails, payment reconciliation, refund rules and subscription entitlements.
- Define store billing by platform and region, without assuming every mobile app can open Stripe Checkout.
- Specify PostHog events, financial data backup and retention, Golden set cost checks and fallback limits.
- Deliver a twelve-month, three-scenario [computed CSV](../../../docs/business/model.csv). This change specifies future implementation; it does not implement billing or change model configuration.

## Capabilities

### New Capabilities

- `subscriptions`: plan catalogue, entitlements, recurring grants, store billing, subscription lifecycle and refunds.

### Modified Capabilities

- `credits`: versioned prices and quotes, free allowance sizing, paid credit provenance, payment fulfillment, reconciliation, cost limits and monetization analytics.

## Impact

- Client: future price quotes, paywall, channel-appropriate checkout, balances by bucket, renewal/cancellation and restore purchases.
- API: future catalogue, entitlements, append-only ledger integration, webhook inbox/outbox, refunds and reconciliation. Postgres remains the source of truth.
- Engine: future complete usage reporting, bounded generation and evaluated provider fallback; no billing authority. OpenRouter's current decimal model IDs differ from the hyphenated fallback IDs in the repository; implementation must verify and correct the routing config before launch.
- Proto: no contract changes in this documentation change; future usage fields require matching API and Engine implementation.
- Infra: future financial backups, restore reconciliation and cost alerting; services stay stateless.
- Evals: future pinned Golden set usage envelopes, narration alignment and fallback quality gates.
- ADRs relied on: [0006](../../../docs/adr/0006-llm-providers-and-evals.md), [0007](../../../docs/adr/0007-credits-and-payments.md), [0009](../../../docs/adr/0009-narration-elevenlabs.md).

## Dependencies and scope

`product-rules-v1` owns removal of effort-based credit rewards and the canonical daily-grant ledger rules. Apply that change before archiving this change. This change deliberately does not remove or rewrite its requirements. The existing base credits specification still contains Reward credits until that dependency lands; those legacy rules are not the proposed product behavior. No effort, XP, Streak, Friend streak or Lesson test result can grant Credits under this proposal; Streak freezes remain earned by effort and automatically applied.

The analytics catalogue update is an implementation prerequisite owned by a follow-up change because this work may edit only `docs/business/` and this change directory. The event contracts here must be incorporated into that catalogue before emitters ship.
