## ADDED Requirements

### Requirement: Lesson tutor sheet events
`tutor_message_sent` SHALL carry boolean `has_lesson_context`. `ui_sheet_opened` SHALL accept `sheet_id=lesson_tutor`. `ui_recovery_selected` SHALL accept `surface=lesson_tutor` with `action` one of `retry`, `reload`, `top_up`, `read_transcript`. Events MUST carry the common properties, honour opt-out and never include message, draft or lesson text.

#### Scenario: Opted out
- **WHEN** the Learner has opted out
- **THEN** none of these events is delivered
