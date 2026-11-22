## ADDED Requirements

### Requirement: Streak rollover scheduling
The worker SHALL judge a learner's ended local day within about one minute after the learner's own local midnight, by reading an indexed due time (`streak_state.next_rollover_at`) kept in step with `decided_through` and the learner's time zone, not by converting every learner on each pass. Changing a learner's time zone MUST recompute the due time. Rollover MUST remain idempotent and safe under concurrent workers. Rebuilding a lost streak projection, outbox resumption and league finalization MAY run at a slower cadence (at most every 10, 5 and 5 minutes).

#### Scenario: Due by the learner's own clock
- **WHEN** the clock reaches local midnight for a learner in Pacific/Kiritimati
- **THEN** that learner is judged and a learner in Pacific/Pago_Pago, still on the earlier day, is not

#### Scenario: Time zone change
- **WHEN** a learner's time zone changes
- **THEN** the next judgement time follows the new zone
