## ADDED Requirements

### Requirement: Quick notes and sheets event catalogue
The analytics catalogue SHALL include `quick_note_created` (carrying `source` restricted to `slide|lesson|chat`, and integer `length`) and `ui_sheet_opened` (carrying `sheet_id` identifying the opened sheet, e.g. `quick_note`). Both events SHALL use Learner UUID distinct IDs and common properties (`platform`, `app_version`, `source`), and respect analytics opt-out. Note text, draft content, slide HTML, and private learner data MUST NOT enter PostHog analytics under any circumstance.

#### Scenario: Quick note created event
- **WHEN** a learner captures a quick note from a slide, lesson, or chat
- **THEN** `quick_note_created` is emitted with the verified source and character length, excluding note text

#### Scenario: Quick note sheet opened
- **WHEN** a learner opens a quick note sheet or panel
- **THEN** `ui_sheet_opened` is emitted with `sheet_id=quick_note` and common properties, excluding note text
