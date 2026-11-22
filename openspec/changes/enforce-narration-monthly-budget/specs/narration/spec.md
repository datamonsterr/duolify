## ADDED Requirements

### Requirement: Monthly provider character budget
When live synthesis is enabled the API SHALL count the characters sent to the narration provider in the current subscription cycle from stored narration assets and completed regenerations, and MUST NOT make a live request that would take the cycle total past the configured stop line (`NARRATION_MONTHLY_CHAR_LIMIT` times `NARRATION_BUDGET_STOP_PERCENT`, defaults 40000 and 95%). Reused or shared audio MUST NOT count.

#### Scenario: Budget exhausted
- **WHEN** a lesson's fresh narration would exceed the stop line
- **THEN** no quote, reservation or provider request is made, the slides show captions only, `narration_budget_exhausted` is emitted and an error log line is written

#### Scenario: Reused audio
- **WHEN** a lesson reuses an existing asset
- **THEN** the cycle count does not change

#### Scenario: Operator regeneration
- **WHEN** the budget is exhausted
- **THEN** requested regenerations stay queued and make no provider request
