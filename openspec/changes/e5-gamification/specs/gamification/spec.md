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

### Requirement: Learner-local streak days from XP events
The API SHALL project Streak days from the immutable XP events' retained learner-local dates. The first completion of a local date SHALL extend the Streak once however many Lessons or Courses complete that day or how many consumers run concurrently. Delayed offline verification SHALL reconcile to the canonical dated event, and a time zone change SHALL neither double count a day nor move the decided horizon backwards. Streaks SHALL NOT grant Credits.

#### Scenario: Several completions on one DST day
- **WHEN** a Learner completes Lessons at 01:30 and 15:00 on a 25-hour local day
- **THEN** the Streak increases by one and `streak_extended` is emitted once

#### Scenario: Late verification of a missed day
- **WHEN** an offline completion from a decided missed local day is verified later
- **THEN** the day becomes Done and the current and longest Streaks are recomputed from dated events

### Requirement: Durable streak rollover
A durable, idempotent worker SHALL judge ended local days; clients SHALL NOT decide misses with midnight timers. A missed day SHALL reset the current Streak to zero, keep the longest Streak, and emit `streak_broken` (`previous_length`) once per missed date. Until the worker runs, reads SHALL return the stored Streak with `rollover_pending` and an as-of time. A lost projection SHALL rebuild from XP events without analytics.

#### Scenario: Worker runs repeatedly
- **WHEN** the rollover worker runs twice after a missed day
- **THEN** the Streak is reset once and one `streak_broken` event exists

### Requirement: Streak read model and calendar
`GET /v1/gamification/streak?month=YYYY-MM` SHALL return the authenticated Learner's current and longest Streak, local date, time zone, as-of time, pending state and labelled calendar days (done, missed, today, pending, future, none). The client SHALL show the S13 Streak panel with loading, empty, welcome-back, pending-rollover and error states, a list alternative, keyboard operation and reduced motion. Other Learners' data SHALL be unreachable.

#### Scenario: Cross-learner read
- **WHEN** a Learner requests the endpoint with another Learner's identifier
- **THEN** only their own Streak is returned

### Requirement: Streak analytics, backup and erasure
`streak_extended` (`streak_length`) and `streak_broken` (`previous_length`) SHALL be allowlisted and catalogued, pass the central consent gate and carry no learner text. Streak projection tables SHALL enter nightly dumps, export with the account and erase on deletion; Redis is not used.

#### Scenario: Opted-out Learner
- **WHEN** an opted-out Learner extends a Streak
- **THEN** state persists and no analytics are delivered

### Requirement: Effort-earned Streak freezes
The API SHALL grant one Streak freeze each time a Streak run reaches the configured milestone while the Learner holds fewer than the configured cap, exactly once per milestone and run. A milestone reached at the cap SHALL be recorded without a grant and SHALL NOT grant Credits or a later freeze. At rollover, each judged missed day of a live Streak SHALL consume one of the Learner's own freezes, preserving (not incrementing) the Streak, and SHALL store one protection record per Learner and local day with a persistent dated notice; without a freeze the Streak SHALL break. There SHALL be no purchase or manual activation path. Inventory, milestone and protection rows SHALL be unique and atomic under concurrent workers, and SHALL be reconstructible from XP events after a restore.

#### Scenario: Freeze earned once
- **WHEN** a Learner below the cap completes the seventh day of a run, even with concurrent consumers
- **THEN** exactly one freeze is added and `streak_freeze_earned` is emitted once

#### Scenario: Freeze at cap
- **WHEN** a Learner at the cap reaches another milestone
- **THEN** no freeze and no Credits are granted and `streak_freeze_earned` is not emitted

#### Scenario: Missed day with a freeze
- **WHEN** a Learner with a freeze misses a local day and the rollover runs repeatedly
- **THEN** one freeze is consumed once, the Streak is unchanged, `streak_freeze_used` is emitted once and the S13 panel keeps a dated notice

### Requirement: Streak freeze view and analytics
`GET /v1/gamification/streak` SHALL additionally return `freezes` (held, cap, milestone_days, next_milestone_in), the latest earned milestone and the three latest dated protection notices, and label protected calendar days `frozen`. The S13 panel SHALL show them with text labels and a list alternative and SHALL offer no buy or activate control. `streak_freeze_earned` (`reason`, `streak_length`, `local_date`) and `streak_freeze_used` (`remaining`, `protected_on`, `streak_length`) SHALL be catalogued and allowlisted before emission, pass the central consent gate and carry no learner text. Freeze tables SHALL enter nightly dumps, export with the account and erase on deletion.


#### Scenario: Looking for a way to buy a freeze
- **WHEN** a Learner opens the Streak panel or calls the API
- **THEN** inventory and notices are read-only and no purchase or activation control or endpoint exists
