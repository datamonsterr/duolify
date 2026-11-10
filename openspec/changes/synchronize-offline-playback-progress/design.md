# Design

## Offline Playback Progress Queue and Synchronization
Offline progress events are non-authoritative. When the client progresses through slides while offline or experiencing transient network failure:
1. Each playback progress action is assigned an immutable stable action key (UUID).
2. The event records `action_key`, `lesson_id`, `version`, `slide_id`, `time_ms`, and `occurred_at`.
3. Events are appended to an account-scoped device pending queue stored in `localStorage` under `player:sync-queue:<learner_id>:<lesson_id>`, retaining the queue until server acknowledgment.
4. While pending events exist, the player UI presents "Waiting to sync" with retry actions, maintaining parity across desktop and mobile layouts.
5. On reconnect or manual retry, the client replays each queued event to `POST /v1/lessons/{lesson}/sync`. Retries and double reconnects replay the identical action key.
6. Upon server acknowledgment, the queued events are removed from device storage and "Waiting to sync" clears.
7. Explicit logout safely clears the device pending queue.

## API Endpoint and Postgres Durability
The endpoint `POST /v1/lessons/{lesson}/sync` requires authentication and course enrollment:
- Validates payload against the immutable manifest: matching manifest version, recognized slide ID, non-negative `time_ms` within the slide's duration, and valid ISO-8601 `occurred_at`.
- Rejects malformed or foreign events (unauthorized lesson, invalid action key format, mismatched learner) with appropriate 400/401/404 status.
- Persists events to `lesson_playback_sync_events` with primary key `action_key`. Duplicate submissions of the same action key by the same learner are idempotent no-ops.
- Merges events into `lesson_playback_positions` out of order: the stored cursor is updated only if the incoming event's `occurred_at` is greater than or equal to the cursor's current `occurred_at`, ensuring monotonic latest-progress state even when events arrive out of order.
- Playback synchronization never awards completion, XP, or streak extension; the last slide links only to the lesson test.

## Stable Slide Deep Links
Navigation to `/lesson/$lessonId?slide=<slide_id>` validates the slide ID against the manifest:
- If valid, the player mounts focused on the referenced slide.
- Deep linking never implies completion or awards XP, even if targeting the final slide.
