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
A Streak freeze SHALL preserve a Streak for one missed day. The system MUST apply an owned freeze automatically at rollover, without Learner action. Freezes MUST be earned through effort: Streak milestones set in config (for example every 7 days), and Friend streak milestones. Freezes MUST NOT be bought with Credits and MUST NOT be granted as Credits. The number held MUST be capped by config.

#### Scenario: Freeze consumed
- **WHEN** a day is missed and the Learner owns a freeze
- **THEN** one freeze is consumed and the Streak is unchanged
- **AND** `streak_freeze_used` is emitted

#### Scenario: Cap reached
- **WHEN** a Learner at the freeze cap reaches another freeze milestone
- **THEN** the freeze is not added and no Credits are granted

#### Scenario: Freeze earned
- **WHEN** a Streak reaches a freeze milestone and the Learner is below the cap
- **THEN** one freeze is added once for that milestone and `streak_freeze_earned` is emitted

#### Scenario: No purchase path
- **WHEN** a Learner looks for a way to buy a freeze with Credits
- **THEN** none exists in the API or the client

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

### Requirement: Weekly league membership from durable XP
A Learner SHALL be assigned to a weekly league group on their first XP event of a league week (Monday to Sunday of their completion-local date), inside the transaction that records the XP event. Groups hold at most the configured size, are formed per week and tier, and a new Learner starts in the first configured tier or the tier of their latest earlier membership. Membership SHALL be unique per Learner and week. Weekly XP SHALL always be the sum of the Learner's `xp_events` whose local date falls in the week; no score is stored. Ranking SHALL order by weekly XP descending, then by earlier group slot, so ties are deterministic and flagged. Redis MAY hold a disposable sorted set of the ranking; if it is missing, stale past its short TTL or unreachable, the ranking SHALL be rebuilt exactly from Postgres. League membership SHALL NOT grant Credits.

#### Scenario: First eligible completion
- **WHEN** a Learner with no membership for the week earns their first XP event
- **THEN** exactly one membership is created, even with concurrent consumers, and no analytics event is emitted for joining

#### Scenario: Redis flushed
- **WHEN** Redis is flushed and the Learner opens Leagues
- **THEN** the same ranks, weekly XP and tie order are returned, rebuilt from Postgres

### Requirement: League read model and S13 view
`GET /v1/gamification/league?offset&limit` SHALL return the authenticated Learner's own group only: state (`active` or `not_joined`), week start/end, as-of time, local date and time zone, tier name, member count, configured promotion and demotion zones, the Learner's rank, weekly XP and zone, and a page of rows (rank, display name, weekly XP, tie flag, self flag, zone) with next/previous offsets. The response SHALL also carry `last_week` for the prior week: `final` (league, rank, members, weekly XP, outcome, next league), `closing` (the week has a membership but is not finalized; shown as "Week is closing") or `none`. The Leagues screen SHALL implement the S13 league states: loading, not joined with a Start a lesson action, active with highlighted self row and text zone boundaries, long names, tie labels, pagination by keyboard, offline cached ranks with an as-of time, and an error with a Retry action, in light and dark and with reduced motion.

#### Scenario: Not joined
- **WHEN** a Learner without XP this week opens Leagues
- **THEN** no group is created and "Complete a lesson to join" with a Start a lesson action is shown

#### Scenario: Cross-learner read
- **WHEN** a Learner requests another Learner's league or group
- **THEN** only their own group is reachable

### Requirement: League analytics, backup and erasure
`ui_screen_viewed` with `screen_id=leagues` SHALL be catalogued (design-system catalogue, screen 15) before the client emits it, pass the consent gate and carry no learner text. League groups and memberships SHALL enter nightly dumps, export with the account and erase on deletion (groups hold no personal data). Redis ranking keys are disposable and are not backed up.

#### Scenario: Opted-out Learner
- **WHEN** an opted-out Learner opens Leagues
- **THEN** the view works and no analytics are delivered

### Requirement: Final weekly league results
Each league group SHALL be finalized exactly once, after every time zone has left the week (Monday 12:00 UTC), in one transaction guarded by the group row lock. Finalization SHALL store each member's final rank, tier before and after, weekly XP and outcome in `league_week_results` (unique per Learner and week; deterministic id), using the configured promotion and demotion zones: ties order by earlier slot, a member with no XP is never promoted, groups too small for a zone keep everyone in place, and the top tier never promotes and the lowest never demotes. The next week's tier SHALL come from the stored result; a Learner who joined the next week early SHALL be moved to the earned tier when the week closes. Retries, concurrent workers and restarts SHALL NOT create a second result or event. Results SHALL NOT grant Credits.

#### Scenario: Rollover under retries
- **WHEN** finalization runs concurrently and repeatedly after a week ends
- **THEN** each group is finalized once, `league_promoted` or `league_demoted` is emitted once per moved Learner and stayed Learners emit nothing

#### Scenario: Rollover twice
- **WHEN** two consecutive weeks close
- **THEN** each Learner's second week starts in the tier earned by the first and the Leagues "Last week" tab shows the latest outcome

### Requirement: Restore rebuilds gamification state
After restoring Postgres from a nightly dump, XP, streak, freeze consumption, protections and league results SHALL be recomputable from `xp_events` and league memberships: the rollover worker rebuilds streak and freeze projections silently and league finalization re-creates missing results with their original event ids. The Leagues view SHALL show the server's prior-week result and no restore path SHALL grant Credits or new rewards.

#### Scenario: Restored fixture
- **WHEN** projection tables are lost and the workers run twice
- **THEN** XP, streaks, freeze consumption and league results equal the originals and no streak analytics are replayed

### Requirement: League outcome analytics and backup
`league_promoted` and `league_demoted` (`event_id`, `week_start`, `league`, `rank`, `from_tier`, `to_tier`) SHALL be catalogued and allowlisted before emission, pass the central consent gate and carry no learner text. `league_week_results` SHALL enter nightly dumps, export with the account and erase on deletion.

#### Scenario: Opted-out Learner promoted
- **WHEN** an opted-out Learner is promoted
- **THEN** the result is stored and shown but no analytics are delivered

### Requirement: Paid learning analytics and history exports
The API SHALL expose learning analytics and history exports derived dynamically from durable progress records (`xp_events`, `lesson_completion_summaries`, `daily_goal_achievements`, `streak_state`, `streak_days`, `streak_day_protections`). Learn subscribers SHALL be entitled to stream their learning history export. Plus subscribers SHALL be entitled to view advanced learning analytics breakdowns by course, local date, streak, and lesson completion. Pro subscribers SHALL be entitled to choose custom export dimensions from a bounded server-side allowlist (`course`, `daily`, `streak`, `completions`, `leagues`). The server MUST enforce verified subscription entitlements and reject unentitled or expired plans with HTTP 403 Forbidden. Analytics and exports MUST NOT expose private course content of other learners.

#### Scenario: Learn subscriber exports learning history
- **WHEN** an authenticated Learner with an active Learn subscription requests learning history export
- **THEN** the API streams an encrypted-in-transit JSON attachment containing durable lesson completions, earned XP, local dates, and streak summaries

#### Scenario: Plus subscriber views learning analytics
- **WHEN** an authenticated Learner with an active Plus subscription requests learning analytics
- **THEN** the API returns course, local-day, streak, and completion breakdowns formatted for the Learner's stored time zone

#### Scenario: Pro subscriber exports custom dimensions
- **WHEN** an authenticated Learner with an active Pro subscription requests custom analytics export with allowlisted dimensions
- **THEN** the API streams a structured JSON export containing only the requested dimensions

#### Scenario: Unentitled or expired plan access
- **WHEN** a Learner with a Free plan or expired subscription attempts to access paid analytics or exports
- **THEN** the request is rejected with HTTP 403 Forbidden and upgrade guidance

### Requirement: Learning analytics UI and telemetry
The client SHALL render desktop and mobile learning analytics and export controls matching canvas designs and tokens, supporting loading, empty, error/retry, keyboard navigation, light/dark themes, and reduced motion. Clicking export actions SHALL emit `ui_control_activated` with `control_id=export_history` or `control_id=export_analytics` and `surface=learning_analytics`. Viewing the screen SHALL emit `ui_screen_viewed` with `screen_id=learning_analytics`. Telemetry MUST pass the central opt-out gate and MUST NOT contain learner text or private course details.

#### Scenario: Learning analytics viewed
- **WHEN** an entitled Learner opens the learning analytics screen
- **THEN** `ui_screen_viewed` is emitted with `screen_id=learning_analytics`

### Requirement: Streak rollover scheduling
The worker SHALL judge a learner's ended local day within about one minute after the learner's own local midnight, by reading an indexed due time (`streak_state.next_rollover_at`) kept in step with `decided_through` and the learner's time zone, not by converting every learner on each pass. Changing a learner's time zone MUST recompute the due time. Rollover MUST remain idempotent and safe under concurrent workers. Rebuilding a lost streak projection, outbox resumption and league finalization MAY run at a slower cadence (at most every 10, 5 and 5 minutes).

#### Scenario: Due by the learner's own clock
- **WHEN** the clock reaches local midnight for a learner in Pacific/Kiritimati
- **THEN** that learner is judged and a learner in Pacific/Pago_Pago, still on the earlier day, is not

#### Scenario: Time zone change
- **WHEN** a learner's time zone changes
- **THEN** the next judgement time follows the new zone
