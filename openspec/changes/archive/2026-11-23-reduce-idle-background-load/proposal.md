# Reduce idle background load

## Why
One small VM runs Postgres, Redis, two API replicas and one worker. Every Maintain loop started in the worker and in each replica, so each sweep ran three times; credits polled every second, the pricing activator every 200 ms, and the per-minute sweeps scanned whole tables (every daily grant under the global financial lock, every learner joined to `pg_timezone_names`, every live friend pair). An empty database already took 1064 statements a minute from one worker and one replica.

## What Changes
- The worker is the only owner of every Maintain loop. API replicas start none; two stay by design: the pricing activator (refreshes the replica's in-memory copy) and the shared-asset sweep (needs object-store keys the worker lacks).
- Polling intervals move to seconds and minutes with idle backoff: credit jobs/outbox 5 s to 30 s, course recovery 10 s, invite push drain 30 s, pricing activator 10 s, friend-streak sweep 5 min.
- Daily credit expiry reads an indexed grant-date window, expires each learner in its own short transaction (the global lock is no longer held across a scan), and wallet reads expire only the reading learner.
- Streak rollover reads an indexed `streak_state.next_rollover_at` kept by triggers, instead of converting every learner through `pg_timezone_names` each minute.
- New partial and expression indexes for the outbox poll, orphaned holds, daily-grant dates, per-day ledger sums, `owner_reference`, unfinalized leagues and live friend pairs.
- No retention deletes are added: no spec or data-inventory entry gives a window for the unbounded non-financial tables (see design).

## Impact
Touches `services/api` (credits, gamification, social, courses, characters, sharing, pricing, `cmd/api`) and one migration. No proto, client or engine change. Ledger semantics are unchanged.

## Data backup, analytics, model reliability
- Backup: the migration adds one `timestamptz` column (`streak_state.next_rollover_at`, derived, recomputed by trigger and by default `-infinity` for rows restored from an older dump) and indexes. No text column, so `internal/backup/anonymize.go` needs no new rule.
- Analytics: no new events. Events keep their keys and consent checks; outbox delivery moves from 1 s to at most 30 s after the row is written.
- No AI feature is touched, so no eval or fallback change.
