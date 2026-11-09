## ADDED Requirements

### Requirement: Complete account export
The API SHALL export all current learner-owned persisted data, including profile and settings, provider identities without provider secrets or subject hashes, session metadata, credit entries and pricing quotes with consent metadata. Authentication codes, refresh hashes, rate-limit hashes, OAuth state/nonce/PKCE material, and server credentials SHALL never appear. The export SHALL be encrypted and available for no more than seven days. Every table with a learner foreign key SHALL be classified as exportable, intentionally excluded with a reason, or financial-retained.

#### Scenario: Export current account
- **WHEN** an authenticated Learner requests an export
- **THEN** the encrypted export contains every classified learner-data table and no authentication secret or hash

#### Scenario: Future learner table
- **WHEN** a migration adds a table with a learner reference
- **THEN** a coverage test fails until its export and deletion policy is registered

### Requirement: Account deletion and financial retention
Deletion SHALL require explicit confirmation, revoke every session, clear device credentials and disposable caches, erase personal account data, request PostHog person deletion, and create a backed-up tombstone that is replayed before a restore is served. Credit ledger rows SHALL remain append-only and retained at least seven years under a pseudonymous subject. Quote terms and consent records SHALL remain immutable while quote learner identity is nulled. Other feature owners SHALL register erasure hooks.

#### Scenario: Confirm deletion
- **WHEN** a Learner confirms account deletion
- **THEN** all sessions are revoked, personal rows are erased, financial history remains pseudonymous and a tombstone is recorded

#### Scenario: Restore after deletion
- **WHEN** a backup containing a previously deleted account is restored
- **THEN** the tombstone is applied before serving data and retained financial history remains immutable

### Requirement: Privacy controls
Settings SHALL provide export, deletion request, status and recovery controls with keyboard and touch access. The client SHALL emit only catalogued `ui_control_activated` events for `export_data` and `delete_account`, include UUID/common properties, suppress them after opt-out and never include learner content.

#### Scenario: Opted-out privacy action
- **WHEN** an opted-out Learner exports or requests deletion
- **THEN** the action works and no PostHog event is delivered
