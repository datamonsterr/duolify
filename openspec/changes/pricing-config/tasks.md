## 1. E2E tests (write first, failing)

- [x] 1.1 Admin flow: draft, validate, submit, second admin approves, scheduled version becomes active at `effective_from`; client `GET /v1/pricing` shows the new version without a deploy.
- [x] 1.2 Adverse change: author cannot approve own price increase; second admin can; notice window enforced.
- [x] 1.3 Quote and spend: a quote taken before a flip settles at the old version; a new quote uses the new version; ledger rows carry the version.
- [x] 1.4 Rollback creates a new version; rollback to a higher price needs two admins.
- [x] 1.5 Invalid config (schema violation, bad Stripe price ID) blocks activation; runtime reload failure keeps last known good.
- [x] 1.6 PostHog test double receives `pricing_version_exposed` once per session and surface, and lifecycle events.

## 2. Integration tests

- [x] 2.1 Resolver: one active version at any instant, boundary at `effective_from`, concurrent flip.
- [x] 2.2 Seed migration loads the E12 table exactly (golden JSON compare against `docs/business/monetization.md` values).
- [x] 2.3 Audit log append-only (UPDATE/DELETE rejected by trigger); reason required.
- [x] 2.4 Stripe verification against the Stripe test mode / mock.
- [x] 2.5 Admin role enforcement on CLI and endpoints; unauthenticated and non-admin rejected.
- [x] 2.6 Redis loss falls back to Postgres; restore drill resolves the same active version.

## 3. Unit tests

- [x] 3.1 JSON Schema cases: missing key, negative credits, non-integer credits, bad currency, duplicate sku, unknown action key.
- [x] 3.2 Adverse-change classifier over every field class.
- [x] 3.3 Rational per-character narration rounding (ceil once).
- [x] 3.4 Client lint rule: no price or credit literal outside fixtures; test fixture for the rule.

## 4. Implementation

- [x] 4.1 Goose migrations: `pricing_versions`, `pricing_audit`, ledger/quote `pricing_version` columns.
- [x] 4.2 Schema, loader, validator, resolver, Redis cache.
- [x] 4.3 Admin subcommand and `/admin/v1/pricing/*`.
- [x] 4.4 `GET /v1/pricing` and quote endpoint changes; client reads from API.
- [x] 4.5 Stripe verification job.
- [x] 4.6 `config/pricing/seed.json` and seed migration. (Obsolete path: the seed shipped as migration `20261109054917_pricing_seed.sql` and there is no `config/` directory; see `services/api/internal/pricing/README.md`.)

## 5. Analytics, backup, reliability

- [x] 5.1 Add events to the `analytics` catalog (separate catalog change per its change-control requirement) and unit-test emitters.
- [x] 5.2 Add pricing tables to `docs/business/data-inventory.md` and the nightly dump scope; retention 7 years.
- [x] 5.3 Alert on config load failure and on scheduled-flip failure.
- [ ] 5.4 Cost-ceiling check in the validator wired to `engine:eval` usage envelopes.
- [x] 5.5 `.env.example` entries (Stripe keys by environment) with comments.

## E12-02 authoritative quotes (issue #55)

- [x] Browser, Postgres and unit quote tests; immutable terms across a flip.
- [x] Config-owned quote policy, integer money/rational audio, input/model bounds.
- [x] Authenticated create/read/consent with expiry; no wallet reservation.
- [x] Responsive quote review, errors, renewal and original version disclosure.
- [x] Existing pricing analytics catalogue/wrapper, UUID/opt-out test-double checks.
- [x] Encrypted restore fixtures include policy, quote and consent; account FK pseudonymization.
- [x] Preserve AI gates: quote-only work invokes no model or provider.

## E12-03 cached client consumption

- [x] `GET /v1/pricing` exposes the current loaded version and its next approved scheduled version; the client refreshes and displays both when present.
- [x] ETag conditional reads return `304` for an unchanged DTO and a new DTO after the database resolver crosses a scheduled effective time.
- [x] Client price/credit literal gate has authored commercial and documented noncommercial rule fixtures.
- [x] Runtime catalogue reload, scheduled activation, and consistency of the next-change display after activation are deferred to E12-06. E12-03 verifies request-time DTO/ETag behavior; it does not add an API scheduler or reload path.
