## ADDED Requirements

### Requirement: Central consent enforcement
Every learner analytics Capture SHALL require a successful injected consent resolver. A missing resolver, unknown learner or resolver error SHALL suppress delivery. Denials SHALL be cached for five seconds in bounded process memory; allowed decisions SHALL be rechecked so consent withdrawal takes effect immediately.

#### Scenario: Ungated lesson or credit emitter
- **WHEN** an opted-out learner triggers generation, retry or credit expiry events
- **THEN** the shared analytics boundary suppresses every event even if the emitter has no local consent check

### Requirement: External deletion intent ledger
Deletion SHALL first atomically append an immutable object containing only a hashed learner reference outside the restored database. Concurrent writes and retries SHALL preserve the first complete record. The ledger SHALL have no delete or expiry operation and SHALL be excluded from snapshot retention rules. Startup and restore SHALL merge this ledger and reapply personal erasure and financial pseudonymization before opening HTTP listeners; replay errors SHALL prevent serving.

#### Scenario: Restore predates account deletion
- **WHEN** a real database dump taken before deletion is restored without a SQL tombstone
- **THEN** the external intent still erases the learner and linked personal data before serving while preserving immutable financial history

### Requirement: Recent authentication and bounded privacy actions
Export and deletion SHALL require a live session authenticated within the preceding 15 minutes, following the pricing recent-auth policy. Token refresh SHALL NOT renew authentication freshness. Each learner SHALL have independent budgets of three export attempts and three deletion attempts per hour shared across sessions and replicas. Stale-auth failures SHALL offer a fresh sign-in in AccountPrivacy; encrypted exports and explicit DELETE confirmation SHALL be preserved.

#### Scenario: Stolen or stale access token
- **WHEN** a valid token belongs to a session last authenticated more than 15 minutes ago
- **THEN** export and deletion return reauth_required without exposing data or deleting the account

#### Scenario: Repeated privacy requests
- **WHEN** a learner exceeds either hourly action budget
- **THEN** that action returns 429 with Retry-After while another learner retains an independent budget
