# Edge Workers

The public-read Worker lives in `workers/public-read/`. The chat Worker has its own Wrangler configuration in `workers/chat/`. Both start disabled. Hyperdrive has a placeholder ID and the checked-in configurations attach no routes.

Run `mise run client:install` from the workspace root to install dependencies. Run `mise run edge:lint ::: edge:test ::: edge:build` for local checks. The test task runs workerd against disposable Postgres and Redis seeded through the Go sharing integration fixture. It uses `duolify_edge_ro` through the Hyperdrive binding. The build type-checks and bundles locally.

For local HTTP development, copy `.dev.vars.example` to `.dev.vars`. Start the migrated Go API with `mise run api:run` on port 4191 and run `mise run edge:dev` on port 4190. For a local Hyperdrive session, apply the [reader runbook](../docs/infra/edge-public-read.md) and supply the DSN in `CLOUDFLARE_HYPERDRIVE_LOCAL_CONNECTION_STRING_MARKETPLACE_DB`. Use `PUBLIC_READ_MODE=go_cache` to exercise the HTTP fallback without a database connection from the Worker.

Set `PUBLIC_READ_EDGE_ENABLED=true` to exercise offload. Mirror Go's `API_ALLOWED_ORIGINS`, `MARKETPLACE_ENABLED`, and Marketplace TTL, search, pagination and ranking settings in Worker vars. Set the same `PUBLIC_READ_ORIGIN_SECRET` on both sides to enable Go-origin cache fills.

| Route                                | Behavior                                                                                                                                                                               |
| ------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /v1/courses/shared-demo`        | Cache the authored Go response within its versioned ETag and freshness policy. Forward conditional and cross-origin requests to Go.                                                    |
| `GET /v1/marketplace/courses[/{id}]` | Apply the Worker flag and rate limit before cache lookup. A fresh hit stays at the edge. On a miss, use Hyperdrive by default and Go in `go_cache` mode or if the database read fails. |
| `GET /v1/public/shares/{token}`      | Go checks viewer access and token validity on every open. CF-04 can verify Turnstile on the same path.                                                                                 |

Marketplace and share responses retain `no-store` and `noindex`. The Worker cache keeps only anonymous successes. Trusted Go cache fills read Postgres directly to establish a single deadline. A moderation hide reaches both Marketplace surfaces within the configured TTL. Go still applies its rate limiter to origin misses and authenticated requests. The Worker uses Cloudflare's approximate per-location rate limiter with 60 requests per address per 60 seconds.

A rollout needs a separate HTTPS Go origin, the owner-authorized Tunnel and Access checks, and matching release flags. Keep request logs off for share and search paths. [ADR 0026](../docs/adr/0026-cloudflare-offload-and-melotts.md)

For local chat development, copy `workers/chat/.dev.vars.example` to `workers/chat/.dev.vars` and run `mise run edge:dev-chat` on port 4192. Point `CHAT_API_ORIGIN` to the migrated Go API on port 4191. Set `CHAT_EDGE_ENABLED=true` on both services and match their `EDGE_SERVICE_SECRET` and `API_ALLOWED_ORIGINS`. `mise run edge:test-chat` runs only the chat tests, including an eviction and wake with the WebSocket still connected. The full `edge:test` task also runs public-read conformance.

The chat Worker holds connections in a SQLite-backed Agent. Go owns credit reservations, settlement and history. The Worker issues no ledger or storage writes for chat content. [CF-05 transport contract](../openspec/changes/cloudflare-offload/design.md#cf-05-client-transport-contract)
