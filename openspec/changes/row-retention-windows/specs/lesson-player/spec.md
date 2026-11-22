## ADDED Requirements

### Requirement: Playback sync event retention
The worker SHALL delete `lesson_playback_sync_events` rows more than 30 days old (`PLAYBACK_SYNC_EVENT_RETENTION`), in batches, safely with several workers. The authoritative position MUST already be merged into `lesson_playback_positions` when the event row is written, so the purge MUST NOT change any position, completion, XP or streak. A replayed event whose key was purged MUST still merge only by event timestamp and never move the position backwards. Purge counts SHALL be an operational log only, with no learner identifier and no PostHog event. Account deletion MUST keep cascading these rows and backup retention is unchanged.

#### Scenario: Old event purged
- **WHEN** the sweep runs and an event is 31 days old and another is 29 days old
- **THEN** only the 31-day-old event is deleted and the learner's position is unchanged

#### Scenario: Two workers sweep together
- **WHEN** two workers sweep at the same moment
- **THEN** every expired row is deleted once and neither sweep fails
