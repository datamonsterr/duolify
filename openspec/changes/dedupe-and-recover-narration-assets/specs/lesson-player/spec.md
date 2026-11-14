## ADDED Requirements

### Requirement: Narration recovery states
The player SHALL show "Narration unavailable" with captions for failed or unreadable audio and offer `Retry narration` (failed) or `Restore narration` (object could not be read), reachable by keyboard and touch with a 44px target on mobile. While recovery is tracked it MUST show a status ("Narration is being prepared." or "Narration is being restored. Captions stay available.") and follow the manifest until audio is ready. Restored audio MUST wait for an explicit Play. Invalid audio MUST NOT offer Restore.

#### Scenario: Restore on mobile
- **WHEN** the Learner presses Restore narration with the keyboard on a 390px viewport
- **THEN** the status text appears, captions remain and Play is offered only after audio is ready
