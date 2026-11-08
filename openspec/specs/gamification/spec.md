# gamification Specification

## Purpose
Reward consistent learning with XP, Streaks, Streak freezes, a daily goal and weekly leagues. Hot counters are fast for many Learners and Postgres stays the system of record.

## Requirements

### Requirement: XP
The API SHALL award XP when a Lesson is completed (Lesson test passed). XP amounts MUST come from config. A perfect Lesson test MUST award bonus XP. XP MUST be awarded once per Lesson completion.

#### Scenario: Lesson completed
- **WHEN** a Learner passes a Lesson test for the first time
- **THEN** XP is added to the Learner total and the weekly league score

#### Scenario: Repeat completion
- **WHEN** the Learner repeats an already completed Lesson
- **THEN** no XP is awarded

### Requirement: Streak
A Streak SHALL be the count of consecutive Learner-local days with at least one completed Lesson. The day boundary MUST use the Learner's stored time zone. A worker MUST roll over Streaks daily.

#### Scenario: Lesson completed today
- **WHEN** a Learner completes their first Lesson of the local day
- **THEN** the Streak increases by one

#### Scenario: Missed day without freeze
- **WHEN** a local day ends with no completed Lesson and no Streak freeze applies
- **THEN** the Streak resets to zero and the longest Streak is kept

### Requirement: Streak freeze
A Streak freeze SHALL preserve a Streak for one missed day. The system MUST apply an owned freeze automatically at rollover. Freezes MUST be earned through effort (see `credits` reward rules) and MAY be bought with Credits. The number held MUST be capped by config.

#### Scenario: Freeze consumed
- **WHEN** a day is missed and the Learner owns a freeze
- **THEN** one freeze is consumed and the Streak is unchanged
- **AND** `streak_freeze_used` is emitted

#### Scenario: Cap reached
- **WHEN** a Learner at the freeze cap earns another freeze
- **THEN** the freeze is not added and the reward is kept as reward Credits or skipped per config

### Requirement: Daily goal
A Learner SHALL set a daily goal in XP. Meeting the goal MUST be shown in the client and MUST emit `daily_goal_met` once per day.

#### Scenario: Goal met
- **WHEN** daily XP reaches the goal
- **THEN** the client shows the goal as complete and the event is emitted once

### Requirement: Leagues
The system SHALL group Learners into weekly leagues ranked by weekly XP. At week end, top Learners MUST be promoted and bottom Learners demoted. Leaderboards MUST be served from Redis and rebuilt from Postgres if lost.

#### Scenario: Week ends
- **WHEN** the weekly rollover job runs
- **THEN** promotions and demotions are stored and `league_promoted` or `league_demoted` is emitted

#### Scenario: Redis flushed
- **WHEN** the leaderboard cache is empty
- **THEN** it is rebuilt from XP rows in Postgres

### Requirement: Gamification analytics events
The system SHALL emit `xp_earned`, `daily_goal_met`, `streak_extended`, `streak_broken`, `streak_freeze_earned`, `streak_freeze_used`, `league_promoted` and `league_demoted` with properties from the `analytics` spec.

#### Scenario: Streak extended
- **WHEN** a Streak increases
- **THEN** `streak_extended` is emitted with `streak_length` and `course_id`

### Requirement: Gamification data backup and retention
XP events, Streak state, freeze inventory, daily goals and league membership MUST be stored in Postgres and covered by the nightly dump. Redis counters MUST be reconstructible from Postgres. XP event rows MUST be kept for the life of the account.

#### Scenario: Restore
- **WHEN** a restore is performed
- **THEN** Streaks and XP totals can be recomputed from stored events and match the restored totals
