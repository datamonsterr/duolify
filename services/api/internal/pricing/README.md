# E12-01 pricing delivery

`pricing_versions` holds the immutable approved launch document and synthetic
development mappings. `api migrate` seeds `usd-launch-2026-11-08`; `api serve`
loads it before opening its listener and refuses absent/invalid active data.
Resolution uses Postgres time. The public DTO strips mappings, advertises
`sales_enabled: false`, includes the next approved scheduled document when one
exists, and supports ETag conditional reads with a 60-second cache lifetime.

E12-01 delivered the read-only launch catalogue. E12-05 adds the audited admin
workflow below; runtime activation/reload, quotes and settlement remain owned by
their respective E12 tickets. E12-07/E12-08 must verify external billing mappings before live sales;
synthetic mappings resolve only with environment exactly `development`.

The client renders wallet policy/empty activity, packs, monthly/yearly plans,
benefits and course-generation cost components from the DTO. It caches only the
public document for session-scoped, explicitly labelled offline display. No
sample balances, purchases, paid generations or AI calls are produced.

`configureAnalytics` is the auth integration boundary: E1-02 supplies the internal
Learner UUID, platform and opt-out. Public browsing does not invent an identity.
Exposure tests exercise the PostHog SDK double, exact common properties,
per-session/surface/version dedupe and opt-out. No learner text is accepted by
the emitter.

Full nightly database dumps include the JSON and mappings. Keep catalogue
history for at least seven years; financial backup snapshots rotate at 35 days.
The integration test dumps with `pg_dump -Fc`, restores into another database,
checks resolver equality, verifies account deletion retains the identity-free
catalogue, and rejects UPDATE/DELETE/TRUNCATE. Historical rollback requires a
new forward version; Goose Down deliberately refuses history destruction.

Official dependency documentation consulted for this implementation:

- https://pkg.go.dev/github.com/santhosh-tekuri/jsonschema/v6
- https://pressly.github.io/goose/documentation/annotations/
- https://docs.sqlc.dev/en/stable/reference/query-annotations.html
- https://golang.testcontainers.org/modules/postgres/
- https://react.dev/reference/react/useEffect
- https://tanstack.com/query/latest/docs/framework/react/reference/useQuery
- https://playwright.dev/docs/test-assertions
- https://posthog.com/docs/libraries/js
- https://vite.dev/guide/features.html#glob-import

No Engine behavior, model routing, evaluation thresholds or fallback gates change.


E12-02 (`#55`) adds authenticated `POST /v1/quotes`,
`GET /v1/quotes/{id}` and `POST /v1/quotes/{id}/accept`.
Quote requests contain an action plus optional model/voice keys and bounded token/
character counts, never learner text. The response pins original config version,
integer content/audio ceilings, eligible bucket, limits, all token-class rates,
rational audio conversion, selected input and expiry. Consent is append-only and
requires database-time freshness; it neither reserves credits nor invokes AI.
E7 must consume this snapshot rather than reprice it. Accepting expired terms
returns 409; create a new quote and request new consent. Reads remain original
through price flips. Ownership is guarded by the existing auth session middleware.

`pricing_quote_policies` is an immutable extension keyed by Pricing config version
so the already-approved E12-01 seed is unchanged. New versions must supply policy;
missing policy/rate fails closed. Development defaults (600 seconds and logical
`default` voice) await owner launch review. Rates are USD nanodollars per token
(including cache writes/reads), derived from the business model, with USD
nanodollars per character for audio. Listing a model/voice does not enable it;
feature eval, provider-route verification and voice provisioning gates still apply.

E12-05 exposes `/admin/v1/pricing/drafts` for create, edit, validate, diff,
submit, approve, reject, list and show. Each request requires a reason and an
authenticated account with a `pricing_admin` grant. Admin requests require a
session authenticated within the previous 15 minutes. Operators grant roles by
setting `PRICING_ADMIN_IDS` to existing account UUIDs; `PRICING_OWNER_ID` enables
the ADR 0011 sole-owner adverse self-approval exception. The CLI shares the same
operations: `api admin pricing <operation> --actor UUID --reason TEXT` (draft and
edit also require `--config FILE`; version operations require `--version ID`).

Drafts live separately from `pricing_versions`. Approval inserts an immutable
approved version; audit rows are append-only and keep a SHA-256 actor pseudonym.
Adverse classification compares learner-facing fields against the active
config. Adverse approval waits at least 72 hours; an author cannot approve an
adverse draft unless they are the sole configured pricing administrator and
owner. Non-adverse self-approval is allowed. Every SKU must meet the 70% margin
floor computed from the version's protected cost ceiling and included Credits.
The workflow emits `pricing_version_created`, `pricing_version_approved` and
`pricing_version_rejected` through the existing server analytics wrapper, with
no reason text or pricing document in event properties.

Official PostgreSQL 18 documentation consulted for the migration's built-in
SHA-256 pseudonym function: https://www.postgresql.org/docs/18/functions-binarystring.html.
