## ADDED Requirements

### Requirement: Durable funded annual allowance scheduling
Annual activation SHALL verify one full funded annual line using pinned Pricing terms and SHALL issue only the first monthly allowance atomically with twelve persisted service-month keys. A River worker SHALL sweep due funded months at start and every minute, clamp UTC anniversaries from the original anchor, stop before paid-through, and append each service-month ledger entry and analytics outbox once across restart/concurrency/restore.

#### Scenario: Outage across February
- **WHEN** a January 31 annual term misses its February and March anniversaries
- **THEN** the worker catches up each once at clamped UTC dates and never issues beyond the twelve funded service months

#### Scenario: Renewal disabled
- **WHEN** canonical annual subscription state turns off renewal
- **THEN** the remaining funded schedule and benefits persist through paid-through and the client shows the next allowance and effective cancellation date

#### Scenario: Refunded term restored
- **WHEN** canonical full-term refund or dispute revocation is recorded and financial backups are restored
- **THEN** no further grants issue, already issued credits remain available, and replay cannot revive revoked entitlements

### Requirement: Annual schedule privacy and recovery
Schedules and revocation/cancellation history SHALL enter encrypted financial backups, retain seven years with 35-day snapshot rotation, export through the account's original order and sever identity on deletion. Grant and entitlement events SHALL reuse the canonical analytics catalogue, UUID identity, opt-out and content-free deduplicated outbox. This change SHALL preserve existing AI evaluation/fallback gates without new AI behavior.

#### Scenario: Learner deleted
- **WHEN** deletion tombstones are applied after restore
- **THEN** financial history remains pseudonymous and no future grant or analytics event is issued to that learner
