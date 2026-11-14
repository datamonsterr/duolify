## ADDED Requirements

### Requirement: Friend streak event catalogue
The analytics catalogue SHALL include `friend_streak_started` (`event_id`, `local_date`), `friend_streak_extended` (`event_id`, `local_date`, `streak_length`) and `friend_streak_broken` (`event_id`, `previous_length`, `reason` in `missed|friendship_ended`), and `streak_freeze_earned` with `reason=friend_streak`. Events use the Learner's UUID as distinct ID and common properties and respect analytics opt-out. Usernames, display names and the other Learner's identity MUST NOT be sent.

#### Scenario: Extended
- **WHEN** both friends complete on a shared day
- **THEN** each friend's analytics receives `friend_streak_extended` with counts and dates only
