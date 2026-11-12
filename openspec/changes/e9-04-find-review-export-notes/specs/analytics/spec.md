## MODIFIED Requirements

### Requirement: Quick notes and sheets event catalogue
The analytics catalogue SHALL include `quick_note_created` (carrying `source` restricted to `slide|lesson|chat`, and integer `length`), `ui_sheet_opened` (carrying `sheet_id`), `quick_note_reviewed` (carrying `note_id`), `quick_note_deleted` (carrying `note_id`), and `ui_control_activated` with `control_id=export_notes`. All events SHALL use Learner UUID distinct IDs and common properties (`platform`, `app_version`, `source`), and respect analytics opt-out. Note text, search queries, draft content, slide HTML, and private learner data MUST NOT enter PostHog analytics under any circumstance.

#### Scenario: Quick note reviewed event
- **WHEN** a learner marks a note as reviewed
- **THEN** `quick_note_reviewed` is emitted with the note UUID and common properties, excluding note text

#### Scenario: Quick note deleted event
- **WHEN** a learner deletes a quick note
- **THEN** `quick_note_deleted` is emitted with the note UUID and common properties, excluding note text

#### Scenario: Export notes control activated
- **WHEN** a learner triggers note export
- **THEN** `ui_control_activated` is emitted with `control_id=export_notes` and surface context, excluding note content
