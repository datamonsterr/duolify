## ADDED Requirements

### Requirement: Friend nudge event
The analytics catalogue SHALL include `friend_nudge_sent` (`event_id` the nudge UUID, `local_date`) with the sender's UUID as distinct ID and common properties, respecting opt-out. It MUST be emitted once per created nudge, not on a replay or a rejection, and MUST NOT carry usernames, display names or the recipient's identity.

#### Scenario: Nudge sent
- **WHEN** a nudge is created
- **THEN** `friend_nudge_sent` carries UUIDs, date and common properties only
