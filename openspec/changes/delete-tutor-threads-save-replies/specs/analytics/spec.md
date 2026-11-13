## ADDED Requirements

### Requirement: Save and delete thread events
`quick_note_created` SHALL carry `source=chat` and `length` for a note saved from a reply, once per note (not on a replayed save). The client SHALL emit `ui_control_activated` with `surface=tutor_chat` and `control_id=delete_thread` when the Learner confirms deletion. Events MUST carry the common properties and Learner UUID, honour opt-out and never include message, note or thread text.

#### Scenario: Opted out
- **WHEN** the Learner has opted out
- **THEN** neither event is delivered
