## MODIFIED Requirements

### Requirement: Cost reliability
The API MUST enforce a per-request, per-day and per-learner spend cap from config, and MUST alert when platform AI cost reaches 80% or 100% of the platform daily budget (initially USD 50), when rolling seven-day cost per active Learner increases by +20%, when cache hit rate falls below 60%, or when paid AI margin falls below 70%. Alert notifications MUST be deduplicated across notification windows, and operational spend counters MUST continue updating independently when analytics is unavailable or a learner opts out.

#### Scenario: Platform budget alert at 80%
- **WHEN** cumulative daily platform provider spend reaches 80% of the daily platform budget
- **THEN** a deduplicated alert is recorded and emitted to analytics if enabled

#### Scenario: Seven-day learner cost increase
- **WHEN** rolling seven-day cost per active learner increases by at least 20% compared to the prior period
- **THEN** an alert is triggered with deduplication

#### Scenario: Cache hit rate degradation
- **WHEN** cache hit rate falls below 60% over the evaluation window
- **THEN** a cache efficiency alarm is recorded and deduplicated

#### Scenario: Paid margin degradation
- **WHEN** paid AI gross margin falls below 70% over the evaluation window
- **THEN** a margin alarm is recorded and deduplicated
