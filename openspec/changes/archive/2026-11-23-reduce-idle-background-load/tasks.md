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
- [x] 2.8 Retry a failed full daily expiry pass and serialize concurrent passes in a worker

## 3. Follow-ups (need an owner decision)
- [x] 3.1 Retention windows for `lesson_playback_sync_events`, `lesson_test_answer_operations` (done in `row-retention-windows`)
- [x] 3.2 Shared-asset sweep (`sharing.MaintainAssets`) runs only in the worker, which holds the application object-store key (`cmd/api/maintenance_ownership_test.go`; idempotence and concurrency in `sharing/retain_shared_assets_through_deletion_and_recovery_integration_test.go`)
- [x] 3.3 Per-learner wallet balance projection (replaces whole-ledger recompute): ticketed as owner-decision follow-up datamonsterr/duolify#348 (credits money path), not part of this change
