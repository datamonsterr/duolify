## 1. E2E tests (write first, failing)

- [ ] 1.1 Admin flow: draft, validate, submit, second admin approves, scheduled version becomes active at `effective_from`; client `GET /v1/pricing` shows the new version without a deploy.
- [ ] 1.2 Adverse change: author cannot approve own price increase; second admin can; notice window enforced.
- [ ] 1.3 Quote and spend: a quote taken before a flip settles at the old version; a new quote uses the new version; ledger rows carry the version.
- [ ] 1.4 Rollback creates a new version; rollback to a higher price needs two admins.
- [ ] 1.5 Invalid config (schema violation, bad Stripe price ID) blocks activation; runtime reload failure keeps last known good.
- [ ] 1.6 PostHog test double receives `pricing_version_exposed` once per session and surface, and lifecycle events.

## 2. Integration tests

- [ ] 2.1 Resolver: one active version at any instant, boundary at `effective_from`, concurrent flip.
- [ ] 2.2 Seed migration loads the E12 table exactly (golden JSON compare against `docs/business/monetization.md` values).
- [ ] 2.3 Audit log append-only (UPDATE/DELETE rejected by trigger); reason required.
- [ ] 2.4 Stripe verification against the Stripe test mode / mock.
- [ ] 2.5 Admin role enforcement on CLI and endpoints; unauthenticated and non-admin rejected.
- [ ] 2.6 Redis loss falls back to Postgres; restore drill resolves the same active version.

## 3. Unit tests

- [ ] 3.1 JSON Schema cases: missing key, negative credits, non-integer credits, bad currency, duplicate sku, unknown action key.
- [ ] 3.2 Adverse-change classifier over every field class.
- [ ] 3.3 Rational per-character narration rounding (ceil once).
- [ ] 3.4 Client lint rule: no price or credit literal outside fixtures; test fixture for the rule.

## 4. Implementation

- [ ] 4.1 Goose migrations: `pricing_versions`, `pricing_audit`, ledger/quote `pricing_version` columns.
- [ ] 4.2 Schema, loader, validator, resolver, Redis cache.
- [ ] 4.3 Admin subcommand and `/admin/v1/pricing/*`.
- [ ] 4.4 `GET /v1/pricing` and quote endpoint changes; client reads from API.
- [ ] 4.5 Stripe verification job.
- [ ] 4.6 `config/pricing/seed.json` and seed migration.

## 5. Analytics, backup, reliability

- [ ] 5.1 Add events to the `analytics` catalog (separate catalog change per its change-control requirement) and unit-test emitters.
- [ ] 5.2 Add pricing tables to `docs/business/data-inventory.md` and the nightly dump scope; retention 7 years.
- [ ] 5.3 Alert on config load failure and on scheduled-flip failure.
- [ ] 5.4 Cost-ceiling check in the validator wired to `engine:eval` usage envelopes.
- [ ] 5.5 `.env.example` entries (Stripe keys by environment) with comments.

## E12-02 authoritative quotes (issue #55)

- [x] Browser, Postgres and unit quote tests; immutable terms across a flip.
- [x] Config-owned quote policy, integer money/rational audio, input/model bounds.
- [x] Authenticated create/read/consent with expiry; no wallet reservation.
- [x] Responsive quote review, errors, renewal and original version disclosure.
- [x] Existing pricing analytics catalogue/wrapper, UUID/opt-out test-double checks.
- [x] Encrypted restore fixtures include policy, quote and consent; account FK pseudonymization.
- [x] Preserve AI gates: quote-only work invokes no model or provider.
