## ADDED Requirements

### Requirement: Synchronize offline playback progress once
The client SHALL queue non-authoritative playback progress events with stable action keys in device storage while offline or encountering network failure, retaining the queue until acknowledged by the server. While pending events exist, the player UI SHALL display "Waiting to sync" with desktop and mobile parity. On reconnection or retry, queued events SHALL be replayed once. The API SHALL authorize requests by course enrollment, reject foreign or malformed events, persist unique events in Postgres, idempotently ignore duplicate action keys, and merge out-of-order events into the authoritative position cursor based on event timestamp. Playback synchronization SHALL NOT award completion or XP. Explicit logout SHALL clear the device queue.

#### Scenario: Reconnect twice replays event once
- **WHEN** an offline playback event is queued and the client reconnects twice
- **THEN** the event is transmitted with its stable action key, reconciled canonically on the server, and "Waiting to sync" clears with exactly one progress action recorded

#### Scenario: Out-of-order merge
- **WHEN** progress events arrive out of chronological order
- **THEN** both events are persisted uniquely and the canonical cursor reflects the latest event by occurred_at timestamp

#### Scenario: Foreign or malformed event
- **WHEN** an event is submitted for a non-enrolled lesson or with invalid schema
- **THEN** the request fails closed without altering stored progress

### Requirement: Stable slide deep links
The player SHALL accept a validated slide identifier deep link via `slide` query parameter. Valid slide identifiers SHALL open the player at the specified slide without implying lesson completion or awarding XP, even on the final slide. The final slide SHALL link solely to the lesson test.

#### Scenario: Deep link to final slide
- **WHEN** a learner opens a lesson deep link to the final slide
- **THEN** the final slide loads without completion and links only to the lesson test
