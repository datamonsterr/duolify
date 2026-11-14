## ADDED Requirements

### Requirement: Aggregate narration settlement
A Lesson's narration SHALL be reserved with one `narration` quote covering its newly synthesized characters and settled once with only delivered characters summed and rounded up once at the configured rate. Failed audio or alignment MUST be uncharged, the text Lesson MUST stay usable and a retry of the same hash MUST NOT charge a second delivered-artifact price.

#### Scenario: One failed slide
- **WHEN** two slides are delivered and one fails alignment
- **THEN** the reservation settles for the two delivered slides' summed characters rounded up once and the failed slide costs nothing

#### Scenario: Retry
- **WHEN** the failed slide is retried and delivered
- **THEN** only its own characters are charged, once
