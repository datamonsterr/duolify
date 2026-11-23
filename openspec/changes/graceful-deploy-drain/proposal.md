# Graceful deploy drain

## Why
A routine deploy restarted the Engine, API replicas and worker and cancelled in-flight work. On staging the API
logged `engine preview failed code=Canceled` during a deploy, a learner's course preview never appeared and
running lesson generations died. Production would repeat this on every release.

## What Changes
- API: SIGTERM fails `/readyz`, stops accepting connections and lets in-flight requests (previews, SSE tutor
  streams) finish for `API_DRAIN_TIMEOUT` (90 s). Its River queues stop together; running jobs get the same window,
  then are cancelled so they return to the queue at once.
- Engine: SIGTERM reports NOT_SERVING, stops accepting and lets in-flight RPCs finish for
  `ENGINE_DRAIN_TIMEOUT_SECS` (90 s).
- Worker: `WORKER_DRAIN_TIMEOUT` (90 s) soft stop, then hard stop.
- API to Engine gRPC retries `UNAVAILABLE` (up to 5 attempts, about 13 s) so a call that lands in an Engine
  replacement is not lost.
- Deploy: the new Engine is healthy before the old one drains (as the API already did); every old container is
  stopped with a 120 s timeout; `stop_grace_period` is 120 s for api, worker and engine and `deploy-plan.sh`
  rejects a grace period shorter than the drain plus 15 s.
- Client: one automatic retry of a preview that failed with a network error, 502 or 503, using a fresh quote that
  must not cost more than the accepted one.

## Impact
API, Engine, compose and deploy scripts, client preview. No migration, proto change, prompt, model or pricing
change. Backup: no data is created or changed. Analytics: no new event. Model testing and reliability: no model
behaviour changes; the retry is bounded by each call's existing deadline and reservation, so evals and fallbacks
are unaffected.
