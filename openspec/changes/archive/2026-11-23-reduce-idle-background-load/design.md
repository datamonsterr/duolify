# Design

## Ownership
Maintain loops are goroutine loops in the worker, not River periodic jobs. A River periodic job costs an insert, a fetch and a completion (about five statements) per run, which is more than the one indexed probe these loops send when idle; River stays for the hourly and daily work that already uses it (daily expiry, annual allowances, backups, course purge). `internal/maintenance.Loop` backs an idle loop off to its maximum and returns to the minimum after a pass that found work. Every pass is idempotent and row-locked, so a second worker (for example during a rolling deploy) double-applies nothing.

## Timing
| Loop | Before | After |
|---|---|---|
| credit payments, reap, outbox | 1 s, 3 processes | 5 s to 30 s, worker |
| daily credit expiry (Go loop) | 1 min, 3 processes, global lock across a scan | River job every 15 min, per-learner lock; wallet reads expire their own learner |
| course recovery | 1 s | 10 s |
| streak rollover | 1 min, join over all learners | 1 min, indexed due read |
| completion outbox sweep, leagues | 1 min | 5 min |
| lost streak projection repair | 1 min | 10 min |
| friend-streak sweep | 1 min, every live pair | 5 min, pairs stale for 15 min |
| characters recover/purge | 1 min / 1 min | 1 min / 1 h |
| invite push drain | 10 s | 30 s |
| pricing activator | 200 ms per replica | 10 s per replica |

A scheduled pricing version has an `effective_from` days ahead and requests read the active version by the database clock, so activation bookkeeping may lag by one tick.

## Daily expiry
A grant is due when its local date has ended. Reservations already expire every past day of the reserving learner. The sweep reads grant dates of the last three days from `credit_entries_daily_grant_date`, and the first pass of each worker process and one pass a day read all of them, so a worker that was down is repaired. The worker records a full pass only after its query and every learner transaction succeed; concurrent passes on the same worker are serialized. A fully spent grant has no expire row, so "no expire row" cannot mark a grant as handled; the date window does.

## Retention
Windows were looked up before any delete was added:
- `credit_outbox`: event keys are kept seven years; properties are already cleared after 90 days (financial retention job). No delete.
- `lesson_playback_sync_events`: inventory says "14-day retention" for the nightly dump, not for the rows. No delete; proposed window 30 days after `occurred_at`.
- `lesson_test_answer_operations`: inventory says life of account. No delete; proposed 7 days after the session ends (idempotency window), owner to confirm.
- `xp_events`: immutable for the account lifetime (streak and league source). No delete.
- `credit_entries`: append-only by trigger; summarising needs finance sign-off.
- `auth_sessions`, `auth_codes`, `auth_rate_limits`: already purged on use in `accounts`.
