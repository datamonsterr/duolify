## ADDED Requirements

### Requirement: Row retention sweep
Rows that only provide client-retry idempotency SHALL be purged by the worker job `row_retention` (daily, unique per period, batched with `FOR UPDATE SKIP LOCKED`, windows and cadence from the environment variables listed in `.env.example`). The per-feature data inventory MUST state each purged table's window. Purging live rows MUST NOT change dump retention, restore checks or account-deletion cascades.

#### Scenario: Inventory current
- **WHEN** a table gains a retention window
- **THEN** `docs/data-inventory.md` names the window, the env variable and the sweep
