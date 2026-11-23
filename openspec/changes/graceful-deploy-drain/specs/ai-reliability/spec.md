## ADDED Requirements

### Requirement: Deploys do not cancel in-flight learner work
On SIGTERM the API, worker and Engine SHALL stop taking new work and let in-flight work finish for a bounded drain
timeout (90 s by default) before exiting. The API MUST fail `/readyz` and refuse new connections; its HTTP request
contexts MUST NOT be cancelled by the signal. The Engine MUST report NOT_SERVING and finish in-flight RPCs. River
queues MUST stop fetching at once, give running jobs the drain window, then cancel them so they are retried.
API calls to the Engine SHALL retry `UNAVAILABLE` up to five attempts within the call's existing deadline and
reservation, and MUST NOT charge a learner twice. The client SHALL retry a preview once after a network error,
502 or 503, only with a fresh quote that costs no more than the accepted one.

#### Scenario: SIGTERM during a slow request
- **WHEN** the API receives SIGTERM while a request is in flight
- **THEN** that request completes, `/readyz` returns 503 and new connections are refused until the process exits

#### Scenario: Engine replaced during a preview
- **WHEN** the Engine is replaced while a preview call is in flight or starting
- **THEN** the in-flight call finishes on the old Engine or the new call is retried on the new one, and the learner sees the preview

#### Scenario: Job outlives the drain
- **WHEN** a River job is still running when the drain timeout ends
- **THEN** its context is cancelled and it returns to the queue as retryable instead of staying running
