## ADDED Requirements

### Requirement: Learner-paid generation in a Course copy
Reservation, settlement and release for a Lesson generated in a Course copy SHALL belong to the generating Learner at the quoted Pricing config version, covering the content and narration components. An insufficient balance MUST be refused before any Engine call and an undelivered component MUST be released.

#### Scenario: Settle on delivery
- **WHEN** the Engine delivers the Lesson
- **THEN** the Learner's reservation settles and no other account's ledger changes
