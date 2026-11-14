## ADDED Requirements

### Requirement: Versions, pins and archived lessons are backed up
Version lineage and changelogs (`publication_drafts`), pins and lock versions (`course_copies`), lesson keys and archived lessons (`course_lesson_stubs`, `course_units`) SHALL be in the nightly encrypted Postgres dump, and a restore MUST resolve every pin and referenced object.

#### Scenario: Restore keeps archived history
- **WHEN** a dump with an updated copy is restored
- **THEN** the pin, lock version and archived lessons are present
