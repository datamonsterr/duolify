## ADDED Requirements

### Requirement: Copy-local generated Lessons are backed up
Lessons generated in a Course copy and their consent records SHALL be included in the nightly encrypted Postgres dump and the versioned asset copy. Restore MUST return them with the copy and the immutable published hash; account deletion MUST erase them.

#### Scenario: Restore keeps copy-local content
- **WHEN** a dump with a generated copy Lesson is restored
- **THEN** the Lesson and its consent record exist and the pin hash is unchanged
