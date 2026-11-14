## ADDED Requirements

### Requirement: Bounded financial reconciliation and recovery
The API SHALL durably bound inbox processing to five attempts with backoff and
dead-letter replay. Daily reconciliation MUST retrieve canonical injected TEST
provider state, verify funds and repair missing payments, funded period grants and
provider refund/dispute objects using existing business keys. Unresolved differences
MUST raise an independent operational alert and payment_reconciliation_failed.
Live provider activation SHALL remain rejected under ADR 0012.

#### Scenario: Missing and duplicate notifications
- **WHEN** a verified payment or refund notification was lost and canonical provider objects are reconciled twice
- **THEN** the missing operation is applied once by business key and the second reconciliation adds no financial rows

#### Scenario: Poison receipt
- **WHEN** five processing attempts fail
- **THEN** the durable receipt becomes a dead letter, raises an alert and can be explicitly replayed in a new bounded cycle

### Requirement: Financial retention maintenance
An independent maintenance job SHALL purge redacted provider payloads at 30 days
and derived analytics properties at 90 days. It MUST preserve financial audit keys,
ledger, catalogue, provenance and funded schedules for at least seven years and
reuse the existing encrypted 35-day financial snapshot class.

#### Scenario: Maintenance after restore
- **WHEN** maintenance runs on old financial rows and expired payloads
- **THEN** only short-lived diagnostic and derived properties are purged and retained history can still explain every operation
