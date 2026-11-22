# Approve Hy4/GLM routing pricing

## Why
On 2026-11-22 the product owner approved the Hy4/GLM pricing version ("Price approve", ADR 0015 amendment). The only active quote policy still routed to Anthropic Sonnet, which the engine refuses, so `POST /v1/courses/preview` returned 502 in every environment.

## What Changes
- Forward migration `20261123110000_hy4_routing_pricing_approved.sql` adds the new immutable approved version `usd-hy4-routing-2026-11-22`: the config of `usd-hy4-routing-2026-11-22-unapproved` with only `version` and `effective_from` (unique, one second later) changed, and that row's quote policy verbatim. The unapproved row is untouched.
- Activation rows are those `RuntimeCatalogue.ActivateDue` writes (activation event at `effective_from`, `version_activated` audit), plus verification evidence. The owner review trail (approved draft, submit and approve audit) is written only when an owner-approved draft already exists, taking `reviewed_by` from it; `PRICING_OWNER_ID` stays out of the database and no gate is bypassed in code.
- Learner credit prices, packs, plans and limits do not change. Future learner-facing changes still use the draft, owner approval and 72-hour adverse-notice flow.
- Tests compare the ACTIVE version, not only the newest migration file, with `engine/config/models.toml`; the startup route check reports no mismatch for provider `openrouter`.
- No new tables, columns, events, prompts, models, provider calls or evals; the rows follow existing financial backup and seven-year retention.

## Impact
API migrations and pricing/credits/backup test fixtures. No admin, engine, client, infra or proto change.
