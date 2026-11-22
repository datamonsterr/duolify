# Design

## Drain budget
Longest legitimate in-flight work is a quoted preview or lesson generation (quote deadline of a few minutes, usually
under 60 s) and a tutor SSE turn. 90 s covers the p99 of those; anything longer is cut and reported as a normal
failure with credits released. `stop_grace_period` is 120 s: drain (90) + hard-stop grace (10) + margin. The
settings are env-tunable; `deploy-plan.sh` keeps grace >= drain + 15 s.

## Why the Engine call was cancelled
HTTP request contexts are not tied to the signal context, so the API never cancelled its own call. The Engine
container had Docker's default 10 s grace and no overlap: `compose up engine` stopped it and started a new one,
dropping every open and every new RPC. Fix: overlap (scale to 2, wait healthy, then drain the old one), a
bounded drain in the Engine, and API-side retry for the gap.

## Retry and charging
Engine calls are stateless computations. A retried call reuses the same reservation (`Reserve` is idempotent per
quote); usage is reported only by the attempt that answered, so nothing is charged twice. Only `UNAVAILABLE`
retries. Client retry: a failed preview has released its reservation, so the same quote is refused; the client
takes a fresh quote and proceeds only when its ceiling is not above the accepted one.

## Lesson generation
`RunLessonGeneration` is a checkpointed state machine behind an advisory lock; reservation is idempotent per
quote, a checkpointed result is completed on retry, and a job killed after the provider started but before the
checkpoint fails with `generation_interrupted` and releases the customer's funds rather than repeat a paid call.
A long drain makes that rare. Jobs cancelled by the hard stop return an error and are retried by River
immediately instead of staying `running` until the one-hour rescue.

## Proxy
Caddy active health checks do not run for dynamic upstreams and a hostname upstream is one unit, so routing
relies on the closed listener (connection refused is retried on another replica for up to 5 s, existing).
`/readyz` returns 503 while draining for any external balancer.
