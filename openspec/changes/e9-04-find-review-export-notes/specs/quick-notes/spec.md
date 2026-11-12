## MODIFIED Requirements

### Requirement: Review and tracking
The Learner SHALL be able to filter notes by Course, Lesson, tag, status, and reviewed state, search note and context text, edit raw note text with optimistic revision conflict detection without overwriting prior enrichment, and mark notes as reviewed once. The client MUST show notes not yet reviewed, provide clear filter feedback, and resolve revision conflicts by displaying both versions for learner selection.

#### Scenario: Filter by tag and unreviewed state
- **WHEN** a Learner filters by a tag or toggles "Not reviewed"
- **THEN** only notes matching the active filter criteria are listed
- **AND** the count reflects filtered results with an option to clear filters

#### Scenario: Edit raw text with optimistic concurrency
- **WHEN** a Learner edits the raw text of a note with an expected version matching the server
- **THEN** the updated raw text is saved, version increments, and prior enrichment is preserved

#### Scenario: Edit conflict detected
- **WHEN** a Learner saves an edit on a note that was modified concurrently or on another device
- **THEN** the server returns 409 Conflict with the current server version
- **AND** the client displays both the local draft and remote version, allowing the learner to keep the selected version

### Requirement: Note data backup and retention
Notes, enrichment, and review state MUST be stored in Postgres and covered by the nightly dump. Notes MUST be exportable by the Learner as cleanly formatted Markdown (separating original raw text from AI summary, tags, and links) and deletable by the Learner on demand or deleted with the account within 30 days.

#### Scenario: Export note as Markdown
- **WHEN** a Learner exports a note or note library
- **THEN** a Markdown document is generated containing raw note text, context, and separate AI additions (summary, tags, related lesson links) when present

#### Scenario: Delete note
- **WHEN** a Learner deletes a note
- **THEN** the note and its enrichment are removed from storage
- **AND** `quick_note_deleted` analytics event is emitted without note content
