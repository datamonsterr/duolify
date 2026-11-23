## Why

Common tier attribution conflicts with feature entry points, making analytics ambiguous. Activation, quote conversion and capped check-in attendance also lack joinable events.

## What Changes

Reserve `source` for the emitting tier and use `entry_point` for feature attribution. Record signup, quote display and acceptance, and each new check-in including a zero-credit capped check-in. Synchronize the example and immersive event catalogue and remove obsolete terminal failure events that conflict with pooled retries.

## Capabilities

### Modified Capabilities
- `analytics`: separate common source from entry points and define funnel events.
- `quick-notes`: rename analytics provenance.
- `course-sharing`: rename enrollment provenance.
- `data-backup`: attribute restore events to the API tier.
- `lesson-test`: align the event catalogue with pooled retry behavior.
- `pricing-config`: keep the actor identity only as the event distinct ID.

## Impact

PostHog consumers must use `entry_point` for affected events. Analytics remains derived data; signup and quote consent remain in the existing backed-up Postgres records, and check-in events use the existing financial outbox. No new AI behavior is introduced.
