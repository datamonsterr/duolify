## ADDED Requirements

### Requirement: Database-time activation without deployment
The API SHALL resolve the approved, non-cancelled pricing version with the greatest `effective_from <= database now()` and expose the next future approved, non-cancelled version. Scheduling SHALL use a unique effective instant, require a future instant, and preserve the existing adverse notice and approval rules. Cancellation SHALL be allowed only before the scheduled instant and SHALL retain the immutable version and audit history. Runtime reload failure SHALL retain the last known good validated catalogue and log an alert. Redis loss SHALL leave Postgres resolution available.

#### Scenario: Concurrent scheduled flip
- **WHEN** database time reaches a scheduled version's `effective_from` during concurrent reads
- **THEN** all reads resolve the same greatest eligible version and the next scheduled value is consistent

#### Scenario: Cancel a pending schedule
- **WHEN** an administrator cancels a schedule before its effective instant
- **THEN** it no longer resolves or appears as next scheduled, while its immutable version and cancellation audit remain

#### Scenario: Reload failure
- **WHEN** runtime loading encounters invalid stored data or a database error
- **THEN** the API serves the last validated catalogue and records an operational alert

### Requirement: Immutable rollback ancestry and once-only activation
Rollback SHALL create a new draft from the selected historical content, record both the currently active parent and the historical rollback source, and use normal submission, adverse classification, notice and approval. Existing quotes, grants, reservations and paid periods SHALL retain their original version. Activation SHALL be recorded once per version and emit `pricing_version_activated` and `price_table_activated` with common UUID properties and no private content.

#### Scenario: Roll back to a higher price
- **WHEN** a rollback raises a learner-facing price relative to the active version
- **THEN** it is classified adverse and cannot become effective until approved under the normal adverse workflow

#### Scenario: Previously accepted quote
- **WHEN** a version activates after a quote was accepted
- **THEN** the quote retains its original version and accepted terms

#### Scenario: Once-only activation
- **WHEN** multiple API instances observe the same activation
- **THEN** a durable unique marker permits only one activation event emission per version
