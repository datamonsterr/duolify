## ADDED Requirements

### Requirement: Friend streak sweep cadence
Friend streak settlement SHALL continue to run after each learner's rollover and XP award. The periodic sweep exists only to repair a missed settlement: it MUST run only in the worker, settle a live pair only when it has gone 15 minutes without a check, and be idempotent across concurrent workers.

#### Scenario: Missed settlement
- **WHEN** a pair's day-end passed but no settlement ran and the pair was last checked more than 15 minutes ago
- **THEN** the sweep settles it once, even with two workers sweeping
