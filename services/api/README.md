# Go API scaffold

The public HTTP router fronts the private gRPC engine. This bootstrap has no auth
or real course/credit behavior. `GET /v1/courses` returns demonstration data;
`POST /v1/courses/preview` forwards a protobuf JSON request such as
`{"topic":"Physics"}` and returns protobuf JSON with snake_case field names.

Copy the root `.env.example` to `.env`; mise loads it. `DATABASE_URL`, `REDIS_URL`
and `ENGINE_ADDR` are required and validated before startup. `ENGINE_ADDR` accepts
`http://host:port` for private plaintext gRPC or `https://host:port` for TLS.
`API_ADDR`, `LOG_LEVEL`, `POSTHOG_API_KEY`, and `POSTHOG_HOST` use the documented
root defaults. An empty PostHog key disables analytics. No learner events are
emitted by this scaffold. CORS allows the Tauri localhost origins and Vite port 1420.

Run all commands from the repository root:

```sh
mise run proto:gen
mise run api:generate       # pinned sqlc; commit generated queries
mise run api:migrate        # embedded Goose application schema + River schema
mise run api:run            # serve
mise run api:build          # services/api/bin/api
mise exec go -- services/api/bin/api worker
mise run api:lint
mise run api:test           # unit tests only, race detector
mise run api:test-e2e       # real HTTP server, in-process gRPC engine
mise run api:test-integration # Postgres 18 via Docker/testcontainers, race detector
docker build -f services/api/Dockerfile services/api
```

The binary accepts `serve`, `worker`, and `migrate`. Run migrations as a separate
deployment step before workers; starting the API never mutates the schema.
SIGTERM/SIGINT drain HTTP requests and River jobs with bounded shutdown time.
`GET /healthz` has no dependencies. `GET /readyz` returns per-dependency
`postgres`, `redis`, and `engine` status (`ok`/`error`), with HTTP 503 on failure;
database details and engine diagnostics are not returned to clients.

`migrations/` owns the learners and append-only credit ledger schema; `queries/`
and `sqlc.yaml` generate `internal/store/db`. Amounts are signed integer units;
pricing, reservation/settlement rules and billing are follow-up features. Reward
and purchased entries retain distinct kinds. Destructive rollback requires a
backup; production backups and restore drills remain infrastructure follow-up
work under ADR 0003. The integration test uses disposable data only.

Dependency and generator versions are pinned in `go.mod`, `buf.gen.yaml` and the
area task file. The existing streaming `*Event` contract needs a narrow Buf
`RPC_RESPONSE_STANDARD_NAME` exception; `engine.proto` is unchanged.
