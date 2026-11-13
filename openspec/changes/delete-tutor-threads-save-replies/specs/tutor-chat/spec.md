## ADDED Requirements

### Requirement: Save a reply as one raw Quick note
The Learner SHALL save a complete or stopped assistant reply from their own thread as exactly one raw Quick note with message and thread provenance and an optional authorized lesson link. Repeating the request MUST return the same note. Saving MUST NOT require Credits. Enrichment MUST stay a separate, separately quoted action owned by `quick-notes`. A reply longer than 2,000 characters MUST be truncated at that bound and say so. Another Learner's message, a user message or an unknown id MUST answer 404 and create nothing.

#### Scenario: Save twice
- **WHEN** the Learner saves the same reply twice, including after a lost response
- **THEN** one note exists and both answers carry its id

#### Scenario: Zero Credits
- **WHEN** the Learner has no Credits
- **THEN** the raw note is saved with status `enrichment_pending`

### Requirement: Delete a thread everywhere
Deleting a thread MUST be owner-only (404 otherwise) and in one transaction remove its messages and every context reference, record a text-free tombstone, and mark notes saved from it `source unavailable`. Later tutor turns MUST NOT include that history. Notes saved from it MUST stay readable. Restoring a backup MUST re-apply the tombstone before serving.

#### Scenario: Later turn after deletion
- **WHEN** a thread is deleted and the Learner asks again
- **THEN** the Engine request contains none of the deleted messages

#### Scenario: Another Learner
- **WHEN** a different Learner deletes the thread id
- **THEN** the answer is 404 and nothing changes

#### Scenario: Restore
- **WHEN** a backup taken before the deletion is restored
- **THEN** the thread and messages are removed again and the note shows source unavailable
