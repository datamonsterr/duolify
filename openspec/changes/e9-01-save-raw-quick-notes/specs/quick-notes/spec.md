## ADDED Requirements

### Requirement: Raw note capture and offline queue
A Learner SHALL create a Quick note from an authorized slide, lesson, or chat source. The note MUST be saved immediately as raw text in Postgres before any enrichment queueing. Status SHALL be set to `enrichment_pending` (and may remain `enrichment_pending` at zero credit balance). Device-side drafts and offline queue MUST be distinguished from server-synced notes. Sync retries MUST use stable action keys for idempotent synchronization. Closing the note sheet or returning to the lesson MUST restore player focus and playback position.

#### Scenario: Save note during slide playback
- **WHEN** a Learner writes a note during a lesson slide
- **THEN** the note is saved with authorized lesson and slide context
- **AND** the note is saved locally before or alongside server synchronization

#### Scenario: Offline note creation and idempotent sync
- **WHEN** a note is created while offline
- **THEN** it is queued on the device with a stable action key and marked as offline draft
- **AND** when connectivity is restored, synchronizing once or multiple times creates exactly one server record

#### Scenario: Zero credit balance
- **WHEN** a note is saved by a Learner with zero spendable Credits
- **THEN** the raw note is persisted with status `enrichment_pending`
- **AND** the client indicates enrichment is pending until credits are available

#### Scenario: Restore focus on closing sheet
- **WHEN** the quick note sheet is closed
- **THEN** focus returns to the triggering lesson player control
- **AND** the lesson slide playback position is preserved
