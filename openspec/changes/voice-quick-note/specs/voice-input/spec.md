## ADDED Requirements

### Requirement: Quick note by voice without a panel
The client SHALL let the Learner dictate a Quick note from the Voice note button without opening the Quick note panel. The transcript MUST be saved immediately as a raw Quick note with the Course, Lesson, Slide index and title, the playback position at press time and the selected region if any, with source `voice` and an Undo in a toast. Too short, nothing understood, offline, daily cap and microphone denied MUST use the existing toasts; offline MUST use the existing Quick note queue. A voice Quick note costs the same Credits as a typed Quick note. Events `voice_quick_note_started`, `voice_quick_note_saved`, `voice_quick_note_cancelled`, `voice_quick_note_failed` and `voice_quick_note_undone` MUST be emitted without transcript text.

#### Scenario: Nothing understood
- **WHEN** transcription returns no text
- **THEN** no note is created, a toast offers retry and `voice_quick_note_failed` is emitted with reason `empty`

#### Scenario: Offline
- **WHEN** the device is offline when the note is released
- **THEN** the note is queued with its anchor and synced once online
