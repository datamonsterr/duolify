## ADDED Requirements

### Requirement: Chat provenance and source-unavailable notes
A Quick note saved from tutor chat SHALL carry `chat_source` with `state` `available` (thread and message ids) or `unavailable`. When the source thread is deleted the note MUST remain readable and exportable, MUST drop thread and message ids, and MUST show "source unavailable" instead of a link. Provenance MUST be included in database dumps and account export; deleted thread text MUST NOT.

#### Scenario: Source deleted
- **WHEN** the thread a note was saved from is deleted
- **THEN** the note text, summary and tags stay and its provenance reads unavailable
