## ADDED Requirements

### Requirement: Durable versioned provider jobs
The API SHALL reserve eligible source allocations and USD capacity with one idempotent job transaction before provider execution. Only authenticated owners MAY reserve accepted, unexpired quotes. Pricing config SHALL supply a five-minute TTL, service minimum, token/character/time/USD bounds and plan concurrency. Workers SHALL enforce API-derived bounds and return content-free provider/model/cache/character/fallback/attempt Usage. Daily, learner and platform counters SHALL remain authoritative in Postgres when Redis or PostHog is unavailable or analytics is disabled.

#### Scenario: Concurrent reservation
- **WHEN** requests race for the same eligible balance or operational capacity
- **THEN** committed holds cannot exceed either balance or configured capacity and repeated acceptance returns the original job

### Requirement: Independent component finalization
Settlement SHALL retain original quote terms, charge delivered token content at its configured minimum up to its ceiling, round aggregate newly synthesized narration characters once, and release failed components. Repeated finalization SHALL return the stored result. Cache/replay components SHALL cost zero. Every provider attempt SHALL retain actual cost separately from customer Credits, including failures. Expired daily allocations released after midnight SHALL expire immediately and never fund tomorrow.

#### Scenario: Price flip and failure
- **WHEN** prices change during a reserved job and only content is delivered
- **THEN** original-version content settles once, audio is released, and actual attempted provider cost is retained

### Requirement: Financial recovery and privacy
Jobs, allocations, usage, counters and the financial outbox SHALL enter encrypted financial backups. Retained ledger and financial history SHALL remain append-only for seven years with account ownership pseudonymized on deletion and exported on request. Restore SHALL preserve exact allocation, balance and cap state. Credits reserved/settled/released/insufficient/exhausted and AI generation events SHALL use the existing allowlisted analytics wrapper and a transactionally deduplicated outbox, respect opt-out and include no learner text.

#### Scenario: Analytics outage
- **WHEN** PostHog is down or the learner opted out
- **THEN** financial finalization and operational limits still work and no private content is emitted

### Requirement: Offline evaluation gate
Recorded mock golden cases SHALL exercise usage/cache envelopes, cap/timeout/fallback behavior and price pinning without provider calls. Live HF/model promotion SHALL remain gated by E10-01 provisioning and E10-06 evaluation.

#### Scenario: Undelivered terminal failure
- **WHEN** bounded attempts fail
- **THEN** the engine returns attempt usage, the API releases customer Credits, and operational spend remains recorded
