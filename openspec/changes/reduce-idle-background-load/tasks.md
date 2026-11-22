## 1. Tests first
- [x] 1.1 Idle statement count test and baseline (1064 per minute, one worker plus one API replica)
- [x] 1.2 API process starts no Maintain loop (`cmd/api`)
- [x] 1.3 Daily expiry in two time zones, two workers, window and full pass (credits)
- [x] 1.4 Outbox once across two workers, empty outbox no-op (credits)
- [x] 1.5 Index usage for every due predicate (credits, gamification)
- [x] 1.6 Rollover by `next_rollover_at`, zone change, concurrency (gamification)
- [x] 1.7 Friend streak sweep stale-only, once across two workers (social)

## 2. Implementation
- [x] 2.1 `internal/maintenance` loop with idle backoff
- [x] 2.2 Credits maintenance, per-learner expiry, claim backoff
- [x] 2.3 Rollover due column, triggers, repair on a slower cadence
- [x] 2.4 Friend streak, characters, courses, invites cadences
- [x] 2.5 Pricing activator 10 s
- [x] 2.6 Move characters, steering and sharing loops to the worker
- [x] 2.7 Migration with indexes

## 3. Follow-ups (need an owner decision)
- [ ] 3.1 Retention windows for `lesson_playback_sync_events`, `lesson_test_answer_operations` (see design)
- [ ] 3.2 Move the shared-asset sweep to the worker once it holds object-store keys
- [ ] 3.3 Per-learner wallet balance projection (replaces whole-ledger recompute)
