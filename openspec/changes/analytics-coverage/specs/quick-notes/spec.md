## MODIFIED Requirements

### Requirement: Note analytics events
The system SHALL emit `quick_note_created`, `quick_note_enriched`, `quick_note_enrichment_failed`, `quick_note_reviewed` and `quick_note_deleted` with properties from the `analytics` spec. Note text MUST NOT be sent to PostHog.

#### Scenario: Note created
- **WHEN** a note is saved
- **THEN** `quick_note_created` is emitted with `entry_point` (`slide`, `lesson` or `chat`) and `length`
