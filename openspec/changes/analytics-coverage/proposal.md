## Why

Common tier attribution conflicts with feature entry points, making analytics ambiguous.

## What Changes

Reserve `source` for the emitting tier and use `entry_point` for feature attribution.

## Capabilities

### Modified Capabilities
- `analytics`: separate common source from entry points.
- `quick-notes`: rename analytics provenance.
- `course-sharing`: rename enrollment provenance.
- `data-backup`: attribute restore events to the API tier.

## Impact

PostHog consumers must use `entry_point` for affected events. Analytics remains derived data; backups and authoritative state are unchanged. No new AI behavior is introduced.
