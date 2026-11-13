# Go API scaffold

The public HTTP router fronts the private gRPC engine. Email-code sign-in protects
Learner routes; course/credit behavior remains a scaffold. `GET /v1/courses` returns demonstration data;
`POST /v1/courses/preview` accepts an owned confirmed `brief_revision` and accepted
`X-Quote-ID` header bound to that revision. It loads the current profile and exact confirmed
brief before calling the engine, validates samples before settlement and never
creates a course/enrollment. Free-form topic/profile/limits input is rejected.
The private request carries the brief, revision/hash, current profile and server-derived
usage limits together; the HTTP response uses snake_case fields.

Copy the root `.env.example` to `.env`; mise loads it. `DATABASE_URL`, `REDIS_URL`
and `ENGINE_ADDR` are required and validated before startup. `ENGINE_ADDR` accepts
`http://host:port` for private plaintext gRPC or `https://host:port` for TLS.
`API_ADDR`, `LOG_LEVEL`, `POSTHOG_API_KEY`, and `POSTHOG_HOST` use the documented
root defaults. An empty PostHog key or `ANALYTICS_OPT_OUT=true` disables server analytics. No learner events are
emitted by this scaffold. CORS allows the Tauri localhost origins and Vite port 1420.

Run all commands from the repository root:

```sh
mise run proto:gen
mise run api:generate       # pinned sqlc; commit generated queries
mise run api:migrate        # embedded Goose application schema + River schema
mise run api:run            # serve
mise run api:build          # services/api/bin/api
# Worker requires BACKUP_S3_* and BACKUP_AGE_RECIPIENT; use its packaged tools:
mise exec -- docker compose --profile workers up -d --build --wait
mise run api:lint
mise run api:test           # unit tests only, race detector
mise run api:test-e2e       # real HTTP/gRPC plus packaged worker backup/restore
mise run api:test-integration # Postgres 18, River, RustFS via disposable containers
mise run infra:build-worker # pinned pg_dump 18.6, age 1.3.2, local IAM initializer
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
backup. The worker now performs encrypted nightly dumps with date/schema names,
atomic S3 publication, idempotent retries and private operations events; see
[backup setup](../../infra/backup/README.md). Production provisioning, retention
and automated drills remain E11 follow-ups. Tests use disposable synthetic data.

Dependency and generator versions are pinned in `go.mod`, `buf.gen.yaml` and the
area task file. The existing streaming `*Event` contract needs a narrow Buf
`RPC_RESPONSE_STANDARD_NAME` exception; `engine.proto` is unchanged.

Email sign-in (E1-02): `POST /v1/auth/code` accepts `email` and
`age_confirmed=true`; development logs the six-digit code without contacting a
provider. `POST /v1/auth/verify` accepts these fields plus `code`, optional
`locale`, `time_zone`, and `analytics_opt_out`. It returns `access_token`,
`refresh_token`, and `expires_in=900`. Send the access token as `Authorization:
Bearer …` to `GET /v1/me` and all `/v1/courses` routes. `POST /v1/auth/refresh`
rotates a `refresh_token`; `POST /v1/auth/logout` revokes its entire session.
Codes last 10 minutes/five attempts, refresh sessions 30 days; reusing a rotated
refresh token revokes the family. Limits are shared in Postgres: three requests
per email/hour, twenty per direct peer IP/hour and sixty verifications per IP/hour.
Forwarded IP headers are deliberately untrusted; a future trusted-proxy deployment
must configure the edge so rate limits retain per-client meaning.

`JWT_SECRET` must contain at least 32 bytes. `DUOLIFY_ENV=production` requires
`RESEND_API_KEY` and a verified `AUTH_MAIL_FROM`; all other environments use the
local log Mailer. Tests use an in-memory MailSink or a local HTTP Resend double,
never the live provider. The Playwright fixture also runs real auth against a
disposable migrated Postgres; `/test/*` mailbox/expiry controls exist only in that
loopback-only test binary, never in the production router. Data expiry, backup
and cascade deletion are recorded in `docs/data-inventory.md`.
