# Railway baseline (template)

Plan: `docs/infra/cost-migration-options.md` §10, ADR 0014. The cutover gate is: Hetzner server-side p95 must not exceed the Railway baseline. Client-side p95 from Southeast Asia is recorded, not gated.

Values are blank on purpose. The real baseline needs the owner's Railway environment and a Southeast Asian vantage point (task 1.1).

## Run

- Server-side: run `mise run infra:load-baseline` with the k6 script pointed at the Railway API (`API_URL`, `MOCK_ENGINE_URL`; see `infra/load/baseline.js`) from a host in the same region as the service.
- Client-side: run the same script from a Singapore, Jakarta or Ho Chi Minh City vantage point through the public hostname.
- Defaults: read mix `READ_RPS=5` for `READ_DURATION=10m` (about 1k DAU at peak), one Tutor chat SSE stream of `SSE_SECONDS=600` with heartbeats every `SSE_HEARTBEAT_SECONDS=20`.

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
| SSE first byte p95 (ms) | | |
| SSE 10-minute stream completed (done event, no 524) | | |
| SSE heartbeats received (expect at least 24) | | |

## Notes

Anything that skewed the run (cold start, noisy neighbour, rate limits hit).
