## ADDED Requirements

### Requirement: Bookmark event catalogue
The analytics catalogue SHALL include `bookmark_added`, `bookmark_removed`, and `bookmark_opened` with `target_type` (`slide` or `lesson`), `course_id`, `lesson_id`, and `slide_id`. All events SHALL use Learner UUID distinct IDs and common properties (`platform`, `app_version`, `source`), and respect analytics opt-out. Private learner text and slide HTML MUST NOT enter PostHog analytics under any circumstance.

#### Scenario: Bookmark added event
- **WHEN** a Bookmark is created
- **THEN** `bookmark_added` is emitted with `target_type`, `course_id`, `lesson_id`, and `slide_id`

#### Scenario: Bookmark removed event
- **WHEN** a Bookmark is removed
- **THEN** `bookmark_removed` is emitted with `target_type`, `course_id`, `lesson_id`, and `slide_id`

#### Scenario: Bookmark opened event
- **WHEN** a Bookmark is opened
- **THEN** `bookmark_opened` is emitted with `target_type`, `course_id`, `lesson_id`, and `slide_id`
