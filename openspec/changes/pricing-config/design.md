## Context

ADR 0007 already says prices live in a versioned, validated, deploy-free config and ledger entries record the version. This change specifies how. Terms follow `CONTEXT.md` (**Pricing config**, **Credits**, **Daily free credits**, **Subscription**).

## Decisions

**Storage.** Postgres is the system of record (ADR 0003). Tables: `pricing_versions` (id, parent_id, content JSONB, content_hash, status, effective_from, created_by, created_at, approved_by, approved_at, change_class, reason), `pricing_audit` (append-only). Versions are immutable once `approved`; drafts are editable by their author only. The repo holds `config/pricing/seed.json` (the E12 table) and a goose migration loads it as version 1 with status `active`, `effective_from` = migration time. After that, repo files never change prices.

**Version ids.** `<currency>-<slug>-<date>-<n>`, seed `usd-launch-2026-11-08`. This id is what `credit_entries.pricing_version`, quotes, reservations, orders and the `price_table_version` event property carry.

**Content shape (JSON, validated by JSON Schema in `services/api/pricing/schema.json`).**

| Section | Keys |
|---|---|
| `meta` | `currency`, `credit_list_value_minor`, `protected_cost_ceiling_usd` |
| `actions` | per action key: `credits` (integer) or `credits_per_unit` (rational) plus `unit`; keys `course_preview`, `course_outline`, `lesson_content`, `narration`, `tutor_short`, `tutor_long`, `quick_note`, `cached_replay` |
| `daily_free` | `credits`, `eligible_actions`, `expires` = `local_day_end` |
| `packs[]` | `sku`, `credits`, `price_minor`, `stripe_price_id` per environment |
| `plans[]` | `sku`, `interval`, `price_minor`, `monthly_credits`, `entitlements[]`, `stripe_price_id`, `store_product_ids` |
| `limits` | generation concurrency per plan, per-free-learner daily cost cap, platform daily AI budget and alarm percents, deadlines |
| `notice` | minimum hours between approval and `effective_from` for adverse changes |

Seed values (approved E12): preview 25, outline 55, lesson content 140, narration 50 per 1,440 new characters, complete standard lesson 190 (derived: content + narration, not stored), tutor short 5, tutor long 8, quick note 2, cached replay 0; daily free 46; packs USD 5/400, 15/1,350, 40/4,000; Learn 9.99 monthly or 96 yearly, 600 credits/month; Plus 19.99 or 192, 1,600; Pro 39.99 or 384, 3,600; concurrency 1/1/2; per-free-learner cap USD 0.08/day; platform alarm USD 50/day at 80% and 100%.

**Adverse change** (computed by the API from the diff against the active version, never declared by the author): any action `credits` increase, daily free decrease, pack `price_minor` increase or `credits` decrease, plan `price_minor` increase or `monthly_credits` decrease, entitlement removed, limit tightened. Adverse versions need approval by a second `pricing_admin` (not the author) and `effective_from >= approved_at + notice`. Non-adverse versions need one `pricing_admin` (the author may approve). Proposed default notice: 72 hours (owner to confirm); existing paid subscribers' renewal terms still follow the `subscriptions` spec.

**Activation.** A version becomes active at `effective_from`. The resolver returns "the approved version with the greatest `effective_from <= t`". Exactly one version resolves at any instant. The API caches the active version and the next scheduled one in Redis and flips lazily on read, with a worker tick as backup; Redis loss falls back to Postgres.

**Rollback.** Rollback never edits history: it creates a new version whose content equals a chosen earlier version and runs the normal approval path (so rolling back to a higher price is adverse and needs two people).

**Stripe mapping.** Stripe Prices are immutable, so a price change means a new `stripe_price_id`. On activation request the API verifies each ID against the Stripe API (exists, active, amount, currency, interval) and blocks on mismatch. Old IDs stay in old versions for existing subscribers. IDs are not secrets.

**Delivery.** `GET /v1/pricing` returns the active version (ETag, `Cache-Control: max-age=60`) plus `next` (scheduled, adverse changes disclosed). Quotes come from the API (`POST /v1/quotes`) and are authoritative. Clients cache the last response with its version id for offline display only.

**Failure handling.** Boot with no valid active version: API refuses to start. Reload failure at runtime: keep last known good, alert, never serve a half-validated version.

**Admin surface.** `services/api` gains an `admin` subcommand: `pricing draft|validate|diff|submit|approve|reject|schedule|cancel|rollback|list|show`. Same operations exist as `/admin/v1/pricing/*` endpoints behind the `pricing_admin` role (ADR 0004). Every call writes `pricing_audit` with required `reason`.

**Analytics.** `pricing_version_exposed` fires once per session, surface and version. Lifecycle events: `pricing_version_created`, `_approved`, `_activated`, `_rolled_back`, `_rejected`.

## Risks

- Margin erosion from a bad draft: mitigated by the validator's cost-ceiling check against the `ai-reliability` cost limits and by two-person approval for adverse changes only. A too-cheap change is not adverse to learners, so it needs a margin check, not a second approver. Open question: should price decreases below a margin floor also need two people?
- Clock skew: `effective_from` compares against database time.

## Migration

Version 1 is seeded by migration. Existing bootstrap ledger rows get `pricing_version = NULL` and are treated as pre-config.

## Open questions

1. Notice window for adverse changes (proposed 72 hours) and whether price decreases below a margin floor need two approvers.
2. Who holds the `pricing_admin` role at launch (owner plus one other)?
3. `outline_replan` (E16) needs a new approved version; see `course-steering`.
4. Currencies and store-localized prices beyond USD (shape supports them).
