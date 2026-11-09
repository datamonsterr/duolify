## ADDED Requirements

### Requirement: Development setup chat pricing key
The versioned Pricing config SHALL support the `setup_chat` action key with proposed platform-paid credit cost of 0 in development configuration explicitly marked unapproved (`approved: false`). No Learner Credits SHALL be deducted for `setup_chat` in this free mode. Production activation of `setup_chat` SHALL require an approved Pricing config version created through E12-10; while disabled in production config, setup MUST retain the accessible form without blocking onboarding.

#### Scenario: Free setup chat quote in development
- **WHEN** a quote is requested for `setup_chat` under the unapproved development pricing configuration
- **THEN** the returned quote specifies 0 Credits ceiling with platform-paid status and zero Learner charge

#### Scenario: Production disabled fallback
- **WHEN** the active approved pricing configuration lacks the `setup_chat` action key
- **THEN** the API signals that setup chat is unavailable and the client falls back to the form

### Requirement: Setup chat pricing analytics
Pricing quote operations and free setup chat requests SHALL NOT emit paid credit deduction events. Interactions with setup chat pricing quotes MUST record only anonymous platform usage metrics through existing `$ai_generation` tracking without private Learner text. Opted-out Learners MUST NOT produce any analytics events.

#### Scenario: Zero-charge analytics privacy
- **WHEN** a Learner completes setup chat at zero cost
- **THEN** no financial deduction or private text is recorded in analytics

### Requirement: Setup chat pricing data backup and retention
Pricing versions containing the unapproved `setup_chat` definition SHALL be stored immutably in Postgres `pricing_versions` and covered by full nightly database backups retained according to the 7-year financial catalogue policy. No transient setup chat state SHALL be written to financial quote or ledger tables.

#### Scenario: Backup of unapproved pricing version
- **WHEN** a database backup is taken
- **THEN** the unapproved development pricing version is preserved with its `approved: false` flag intact
