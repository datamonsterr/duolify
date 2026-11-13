## ADDED Requirements

### Requirement: Course copies in backups
`course_copies` (pins) and copied Lesson rows MUST be in the nightly dump and the privacy export; erased with the Learner's account. A copy MUST survive deletion of the source Course or its owner. docs/data-inventory.md MUST list the table.

#### Scenario: Restore
- **WHEN** Postgres is restored
- **THEN** every copy resolves its pinned version and lessons without the source Course
