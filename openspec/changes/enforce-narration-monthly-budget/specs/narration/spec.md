## ADDED Requirements

### Requirement: Monthly provider character budget
When live synthesis is enabled the API SHALL reserve the worst-case characters for each provider request atomically in a durable, append-only ledger shared across processes, including up to two provider attempts, and count unknown outcomes conservatively, and MUST NOT make a live request that would take the cycle total past the configured stop line (`NARRATION_MONTHLY_CHAR_LIMIT` times `NARRATION_BUDGET_STOP_PERCENT`, defaults 40000 and 95%). Reused or shared audio MUST NOT count. Successful responses reconcile the reservation to actual attempts; uncalled reservations are released and uncertain outcomes remain counted. Asset purges MUST NOT change usage.

#### Scenario: Budget exhausted
- **WHEN** a lesson's fresh narration would exceed the stop line
- **THEN** no quote, reservation or provider request is made, the slides show captions only, `narration_budget_exhausted` is emitted and an error log line is written

#### Scenario: Reused audio
- **WHEN** a lesson reuses an existing asset
- **THEN** the cycle count does not change

#### Scenario: Operator regeneration
- **WHEN** the budget is exhausted
- **THEN** requested regenerations stay queued and make no provider request

#### Scenario: Concurrent admissions
- **WHEN** two processes request more than the remaining allowance together
- **THEN** atomic admissions prevent their combined reservations from exceeding the stop line

#### Scenario: Unknown outcome or asset purge
- **WHEN** a provider call has an unknown outcome or its asset is purged
- **THEN** its admitted characters remain counted conservatively in the cycle

#### Scenario: Refusal before any provider request
- **WHEN** the engine refuses a call as not promoted
- **THEN** the reservation is released and the slide falls back to captions only

#### Scenario: Abandoned reservation
- **WHEN** a process dies after reserving and never settles
- **THEN** the full reservation stays counted for its cycle (conservative) and is never reopened, so a stale reservation cannot allow overspend
