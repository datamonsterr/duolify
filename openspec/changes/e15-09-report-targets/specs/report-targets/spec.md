## ADDED Requirements

### Requirement: Private reports
The API SHALL let a signed-in learner report a course, a review or a user (by username) with a configured reason and optional text of at most 500 Unicode characters. A reporter MUST have at most one open report per target (a repeat is merged) and at most ten submissions per rolling 24 hours. Reporting yourself or your own content MUST be refused. No API response to the owner or the target SHALL reveal a reporter or report text.

#### Scenario: Duplicate
- **WHEN** the same learner reports the same course twice
- **THEN** one open report exists and the response says it was merged

#### Scenario: Daily cap
- **WHEN** a learner submits an eleventh report within 24 hours
- **THEN** the API answers 429 with Retry-After and stores nothing

### Requirement: Hide at distinct-reporter threshold
When distinct reporters of a course reach the configured threshold (default three) the course MUST be hidden from marketplace lists, the Course page and the Redis catalogue immediately, and one moderation queue item MUST exist ordered by severity then age. Concurrent reports MUST yield exactly one hide and one queue item. Hiding MUST NOT delete or block existing copies.

#### Scenario: Third reporter
- **WHEN** a third distinct learner reports a listed course
- **THEN** the next catalogue read omits it and copies stay usable

### Requirement: Report analytics, backup and erasure
The API SHALL emit `course_reported` (reason, target_type, course_id when known) and `course_hidden_by_reports` (course_id, reporter_count) with UUIDs and closed enums only, honouring opt-out and never including report text. Reports SHALL be in nightly dumps and kept three years; erasing a reporter MUST remove their text and attribution.

#### Scenario: Reporter erased
- **WHEN** a reporter deletes their account
- **THEN** the report remains with no reporter and no text

### Requirement: Report sheet
The client SHALL show "Report" entry points on the course menu and on each review, opening a sheet (desktop dialog, mobile bottom sheet) with the reason radio group, optional details, Send report, loading, success, duplicate, rate-limit, offline retry and neutral unavailable states, keyboard and focus trapped, honouring dark mode and reduced motion.

#### Scenario: Offline
- **WHEN** sending fails offline
- **THEN** the sheet keeps the reason and details and offers Retry
