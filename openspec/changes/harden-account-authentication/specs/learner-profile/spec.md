## ADDED Requirements

### Requirement: Bounded setup chat draft fields
Setup chat SHALL reject draft text exceeding the configured profile text limit (2000 Unicode characters by default) and code fields exceeding 64 characters, with reminder time limited to five characters. Invalid drafts SHALL be rejected before engine invocation or persistence. This validation SHALL add no analytics events or learner text to analytics, and SHALL leave AI fallback and eval behavior unchanged.

#### Scenario: Oversized unconfirmed draft
- **WHEN** an authenticated caller submits an oversized draft field
- **THEN** the API returns a bad request without calling the setup chat engine or saving the draft
