## ADDED Requirements

### Requirement: Authoritative completion XP projection
The API SHALL consume lesson_completion_outbox joined to lesson_completion_summaries, award configured base XP plus the retained first-try-perfect bonus exactly once per learner and lesson, and retain immutable XP events for the account lifetime. Database uniqueness SHALL protect concurrent consumption. Replay SHALL award neither XP nor Credits. Errors SHALL remain visible and pending work SHALL resume after restart.

#### Scenario: Concurrent completion consumption
- **WHEN** multiple consumers process the same learner and lesson concurrently
- **THEN** exactly one XP event exists with the configured award and Credits remain unchanged

### Requirement: Learner-local configured daily progress
The authenticated learner SHALL read XP totals and choose only configured goal options. The consumer SHALL retain completion-local dates using stored IANA timezones, including DST boundaries. A unique daily marker SHALL emit daily_goal_met once per learner-local date. All endpoints SHALL use the authenticated learner and reject cross-learner access.

#### Scenario: Goal reached across a DST boundary
- **WHEN** completions occur on either side of a local midnight or repeated DST hour
- **THEN** XP belongs to the correct local date and the goal marker occurs at most once for each date

### Requirement: Completion analytics privacy
The API SHALL allowlist and catalogue xp_earned, daily_goal_met and lesson_completed with authoritative XP, internal UUIDs and common properties. The central consent gate SHALL suppress opted-out learner delivery. No learner text SHALL be captured.

#### Scenario: Opted-out completion
- **WHEN** an opted-out learner completes a lesson
- **THEN** XP and goal progress persist but no completion or XP analytics are delivered

### Requirement: XP backup and erasure
XP events and daily goal markers SHALL enter nightly encrypted Postgres dumps, export with the account, and erase on account deletion. Restore SHALL reconstruct the same totals and preserve deduplication. No AI reliability or provider behavior SHALL change.

#### Scenario: Restored completion replay
- **WHEN** a restored completion is consumed again
- **THEN** totals and the daily marker are unchanged
