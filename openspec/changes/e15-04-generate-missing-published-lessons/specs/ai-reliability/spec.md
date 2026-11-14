## ADDED Requirements

### Requirement: Published-copy missing Lesson reliability
The Golden set SHALL contain recorded offline cases for a missing Lesson in a published copy: success, schema failure with bounded fallback, gate refusal (previous not passed) and insufficient credits. Each MUST enforce schema checks, the deadline, attempts and USD caps from the quote, and refuse before any provider call when a gate fails.

#### Scenario: Gate refusal makes no provider call
- **WHEN** the previous Lesson is not passed or credits are insufficient
- **THEN** the check fails and the recorded provider is never consumed
