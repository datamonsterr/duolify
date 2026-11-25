## ADDED Requirements

### Requirement: Formatted note and place in the Lesson
After the raw save, enrichment SHALL also return a short `title` and a `formatted_body` produced from the transcript with Course, Lesson and Slide context: dictation errors fixed, the Course's terms, bullets, formula or code where the subject calls for it, in the Learner's language, adding no fact absent from the transcript or Slide. The raw text MUST stay stored and viewable ("Show original") and MUST NOT be overwritten. Enrichment failure MUST leave the raw note with status `enrichment_failed`. Credits MUST follow the existing `quick_note` reservation with no price change. The Notes page SHALL show Course, Lesson and Slide for an anchored note and a timestamp chip that opens the Lesson at that Slide and time. `title` and `formatted_body` MUST follow the note's backup, export, retention and erasure and MUST be blanked by the backup anonymiser.

#### Scenario: Formatted voice note
- **WHEN** a voice note on a chemistry Slide is enriched
- **THEN** the note shows a short title and a formatted body using the Course's terms, and "Show original" shows the raw transcript

#### Scenario: Open from the chip
- **WHEN** the Learner taps the timestamp chip
- **THEN** the Lesson opens on that Slide at that time

#### Scenario: Formatting fails
- **WHEN** formatting times out or fails a check
- **THEN** the raw note is kept, no Credits are charged and retry is offered
