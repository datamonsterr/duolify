## Why

The owner approved the E12 prices (ADR 0007). `monetization-model` defines the catalogue values; nothing yet defines how those values are stored, changed, approved, audited and delivered to clients. Without that, prices end up hard-coded in clients, changing a price needs a deploy, and nobody can answer "which price did this learner pay, and who changed it?". E15 (sharing: on-demand lesson generation for a Course copy) and E16 (steering: re-plan) both charge credits and must read prices from one place.

## What Changes

- New capability `pricing-config`: one versioned **Pricing config** holds every action cost in credits, the daily free credit amount, credit packs, subscription tiers, their benefits and limits, and the Stripe price ID mapping.
- Seed version = the approved E12 table in `docs/business/monetization.md` (catalogue id `usd-launch-2026-11-08`). No value is invented here.
- Schema validation at load, immutable versions, `effective_from` scheduling, admin CLI and admin endpoint with audit log, two-person approval for adverse changes (price increases), rollback by new version, price version stored on every ledger entry and quote.
- Clients fetch current prices from the API; no price literal in client, API logic or Engine.
- PostHog events for price version exposure and for config lifecycle; backup and retention for versions and audit log.
- Small delta to `credits`: ledger entries and quotes reference the Pricing config version.

## Capabilities

### New Capabilities

- `pricing-config`: versioned schema-validated pricing, admin change flow, approval, audit, scheduling, rollback, client delivery, Stripe mapping, analytics, backup.

### Modified Capabilities

- `credits`: binds prices, quotes and ledger entries to a Pricing config version.

## Impact

- Client: reads `GET /v1/pricing` for paywall, pack list, plan list, action cost labels; renders server quotes; never computes or stores prices as constants. Lint rule forbids price literals.
- API: Pricing config store (Postgres), loader/validator, resolver ("active version at time t"), admin subcommand and `/admin/v1/pricing` endpoints, audit log, Stripe price verification, Redis cache of the active version.
- Engine: no access to commercial values; receives only token and character limits that the API derives from config. No billing authority.
- Proto: none. If bounds are passed to the Engine they ride on existing request fields; any new field needs a matching change in `services/api` and `engine`.
- Infra: admin access path and role (`pricing_admin`), backup of pricing tables, alert on config load failure. No secrets in config.
- Evals: cost-envelope checks use the active config's ceilings (see `ai-reliability` cost limits).
- ADRs relied on: [0003](../../../docs/adr/0003-data-stores.md), [0004](../../../docs/adr/0004-auth.md), [0007](../../../docs/adr/0007-credits-and-payments.md).

## Dependencies and scope

- Depends on `monetization-model` (catalogue values and `credits`/`subscriptions` requirements). That change's "versioned catalogue" is realized by this capability; its `price_table_version` event property is the Pricing config version id.
- Consumed by `course-sharing-marketplace` and `course-steering`.
- Out of scope: changing any approved value, tax, new currencies and localized store prices (the config shape allows them; values are a later change), A/B price experiments (exposure analytics only).
