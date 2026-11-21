# Railway baseline (template)

Plan: `docs/infra/cost-migration-options.md` §10, ADR 0014. The cutover gate is: Hetzner server-side p95 must not exceed the Railway baseline. Client-side p95 from Southeast Asia is recorded, not gated.

Values are blank on purpose. The real baseline needs the owner's Railway environment and a Southeast Asian vantage point (task 1.1).

## What is measured

`mise run infra:load-baseline` starts a throwaway compose project (Postgres, Redis, object store, the real API) with a **mock Engine behind the API** (`infra/load/compose.yml`, gRPC per `proto/`). It signs in one dev learner through the real `/v1/auth/*` routes, seeds a course, a lesson with a manifest and credits (`services/api/cmd/loadseed`, reusing `playerfixture`), then runs two legs at the same time:

1. **Read mix** (k6, `infra/load/baseline.js`): `READ_RPS=5` for `READ_DURATION=10m`, about 1k DAU at peak. Weights:

| Route | Weight | Auth | Why |
|---|---|---|---|
| `GET /v1/courses` | 15 | yes | home / course picker |
| `GET /v1/courses/{id}` | 20 | yes | learn path |
| `GET /v1/lessons/{id}/manifest` | 20 | yes | opening a lesson |
| `GET /v1/gamification/streak` | 15 | yes | streak / progress |
| `GET /v1/me` | 8 | yes | session bootstrap |
| `GET /v1/me/credits` | 7 | yes | wallet badge |
| `GET /v1/courses/shared-demo` | 5 | no | public demo |
| `GET /v1/pricing` | 4 | no | public |
| `GET /v1/features` | 3 | no | public |
| `GET /v1/characters/default-cast` | 2 | no | public |
| `GET /healthz` | 1 | no | probe |

   Thresholds: error rate below 1 %, p95 below 1 s, every check passes.

2. **Tutor chat stream through the API** (`infra/load/sse-stream.mjs`): quote, accept, then `POST /v1/courses/{id}/tutor/messages`. The mock Engine keeps the reply silent for `SSE_SECONDS=600`, so only the API's heartbeats (`SSE_HEARTBEAT_INTERVAL`, default 15 s) keep the stream alive. The script reads the stream incrementally and records the **maximum gap between any two received chunks**. It fails the run if the gap is 25 s or more, the status is not 200, or no `done` event arrives. It is a Node script because k6 2.x has no SSE or incremental-read API; Node is already in the mise toolchain. Later, point `API_URL` at the Cloudflare-proxied hostname to measure the proxy path.

Notes on the stack: the launch pricing catalogue caps a Tutor reply at 30 s, so `loadseed` adds an approved pricing version (cloned, only `tutor_note_deadline_seconds` raised) in the throwaway database before the API starts. The API server's 20 s `WriteTimeout` is lifted per stream by each heartbeat (`internal/sse`).

## Run

- Server-side: `mise run infra:load-baseline` on a host in the Railway region. For an existing environment set `API_URL` and skip the stack; the seed needs a throwaway database, so for Railway use a seeded staging learner (`ACCESS_TOKEN`, `COURSE_ID`, `LESSON_ID`) and run `k6 run infra/load/baseline.js` plus `node infra/load/sse-stream.mjs` directly.
- Client-side: the same two commands from a Singapore, Jakarta or Ho Chi Minh City vantage point through the public hostname.
- Short local variant: `mise run infra:load-baseline-test`.
- Defaults: `READ_RPS=5`, `READ_DURATION=10m`, `SSE_SECONDS=600`, `SSE_HEARTBEAT_INTERVAL=15s`, `MAX_GAP_SECONDS=25`. `LOAD_SUMMARY=<file>` and `SSE_RESULT=<file>` keep the k6 summary and the stream result as JSON.

## Environment

| Field | Value |
|---|---|
| Date | |
| Commit | |
| k6 version | |
| Railway region / plan | |
| Vantage point (server-side) | |
| Vantage point (client-side) | |

## Results

| Metric | Server-side | Client-side (SE Asia) |
|---|---|---|
| Read mix requests | | |
| Read mix error rate | | |
| Read mix p50 (ms) | | |
| Read mix p95 (ms) | | |
| Read mix p99 (ms) | | |
| Authenticated share of requests | | |
| SSE first byte (ms) | | |
| SSE 10-minute stream completed (done event, no 524) | | |
| SSE heartbeats received (expect at least 40 at 15 s) | | |
| SSE max gap between chunks (s, must be below 25) | | |

## Notes

Anything that skewed the run (cold start, noisy neighbour, rate limits hit).
