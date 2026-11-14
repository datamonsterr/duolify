## Why
Two friends should keep one shared Friend streak that grows only when both learn (#94). It must follow each friend's own local day and the later day-end, reuse the personal Streak freezes from E5 instead of a second inventory, and end with the friendship.

## What Changes
- Postgres `friend_streaks` (one row per friendship pair: current/longest days, run number, cursor of the last settled local dates), `friend_streak_days` (idempotent pair-day key per settled day: `extended`, `protected` or `broken`) and `friend_streak_milestones` (idempotent milestone key per pair run). Config column `gamification_config.friend_streak_milestone_days` (default 7).
- A pure evaluator projects both learners' `streak_days`, `streak_day_protections` and `streak_state.decided_through` into the shared streak. It runs in the pair transaction (`lockLearners`) after a completion, after a personal rollover and in a periodic sweep. Every key is unique in Postgres, so replays and concurrent runs are no-ops.
- Grow once per shared day when both complete; wait for the later day-end before breaking; a missed side must have its own E5 protection; milestone freezes are granted to both learners as `streak_freeze_milestones(kind='friend_streak')` below the E5 cap, never as Credits.
- `removeFriendshipTx` deletes the pair state (so Remove, Block and account deletion all end the streak) and reports a `friend_streak_broken(reason=friendship_ended)` event.
- Friends list gains a `friend_streak` view (length, each friend's own local date and today status, state) gated on a friend viewer, the friend's `show_streak_to_friends` and no block; the personal streak is wired into the same projection.
- Client: Friends screen (S14) shows the Friend streak with both local-day statuses on every row, desktop and mobile, all states.
- Analytics: `friend_streak_started`, `friend_streak_extended`, `friend_streak_broken`, and `streak_freeze_earned(reason=friend_streak)`.
- Data: new tables in nightly dumps, privacy policy, export, backup fixture, inventory.

## Out of scope
Nudges and account-deletion UX (E6-07), credit rewards (removed by ADR 0007), push notifications.
