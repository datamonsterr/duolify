# E12-01 pricing delivery

`pricing_versions` holds the immutable approved launch document and synthetic
development mappings. `api migrate` seeds `usd-launch-2026-11-08`; `api serve`
loads it before opening its listener and refuses absent/invalid active data.
Resolution uses Postgres time. The public DTO strips mappings, advertises
`sales_enabled: false`, includes the next approved scheduled document when one
exists, and supports ETag conditional reads with a 60-second cache lifetime.

This ticket delivers the read-only launch catalogue. Scheduling/admin workflows,
runtime activation/reload, quotes and settlement belong to subsequent E12
tickets. E12-07/E12-08 must verify external billing mappings before live sales;
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
