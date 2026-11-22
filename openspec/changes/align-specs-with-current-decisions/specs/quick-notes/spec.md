## MODIFIED Requirements

### Requirement: Create a Quick note
A Learner SHALL create a Quick note from a Lesson, a Slide or Tutor chat. The note MUST be saved immediately as raw text, before enrichment runs, and MUST work offline and sync later.

See `Raw note capture and offline queue` for related detailed requirements.

#### Scenario: Note during a Slide
- **WHEN** a Learner writes a note on a Slide
- **THEN** the note is saved with `lesson_id` and `slide_id` and is visible without waiting for AI
