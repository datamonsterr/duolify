## ADDED Requirements

### Requirement: Automatic attendance check-in Credits
The first authenticated app open of a new eligible local calendar day MUST call the idempotent check-in endpoint and grant 150 Credits plus a one-time 250 first-check-in bonus (400 on day one). Attendance is not Lesson effort. Amounts and seven bank days MUST come from approved versioned Pricing config. Check-in sources MUST fund every action purchased Credits fund, after eligible Daily free credits and before oldest eligible paid sources. The existing 46-Credit daily allowance, eligibility and expiry MUST remain unchanged.

#### Scenario: First open and retry
- **WHEN** a new Learner opens the app and two devices retry check-in concurrently
- **THEN** exactly one attendance grants 400 Credits, one lifetime bonus exists, and only the winning response triggers a non-blocking localised status confirmation with amount and balance

#### Scenario: App remains open or reconnects
- **WHEN** the returned day boundary arrives or connectivity returns
- **THEN** the client retries check-in without blocking Learn and without granting missed offline days

### Requirement: Atomic check-in bank cap
The regular grant MUST equal min(configured daily grant, max(0, daily grant times bank days minus unspent check-in Credits including reservations)). The first-time bonus MUST then be granted in full once and count toward all later cap calculations. Releases and refunds MUST preserve their source and MUST NOT destroy Credits even if the bank subsequently exceeds the cap. Check-in Credits MUST NOT expire.

#### Scenario: Cap boundary
- **WHEN** an existing Learner checks in with 1049 banked check-in Credits
- **THEN** exactly 1 Credit is granted; at 1050 a new attendance grants zero and the client confirms the bank is full

#### Scenario: Bonus exception
- **WHEN** a first check-in has only 1 Credit of regular cap room
- **THEN** it grants 1 regular Credit plus the full 250 bonus and subsequent regular grants remain zero while the bank exceeds 1050

### Requirement: Durable local-day attendance
Attendance MUST have a unique Learner/local-date key and transactionally enforce the cap and lifetime bonus across replicas. A successful attendance MUST freeze the next midnight in the effective profile time zone. A time-zone change MUST NOT advance that boundary. After the boundary the effective zone determines the date; already attended dates MUST remain deduplicated. Ordinary 23-hour and 25-hour DST days MUST remain eligible at local midnight without a fixed 24-hour cooldown.

#### Scenario: Time-zone change and DST
- **WHEN** a Learner changes zone during an attended day or crosses a DST midnight
- **THEN** a zone change cannot grant within the frozen window, and the next unclaimed eligible local date grants at its midnight

### Requirement: Check-in privacy and backup
Attendance and eligibility state MUST be in Postgres backups, export and erasure coverage and the restore anonymizer registry. Financial ledger rows MUST retain existing pseudonymous retention. Account deletion and re-registration MAY receive a new account's bonus; no identity tracking SHALL be added.

#### Scenario: Restore and deletion
- **WHEN** an account is exported, deleted or restored
- **THEN** attendance is exported, removed on deletion and dedupe/bonus financial history is consistent after restore with deletion tombstones applied
