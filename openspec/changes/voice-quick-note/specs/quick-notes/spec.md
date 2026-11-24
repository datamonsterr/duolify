## ADDED Requirements

### Requirement: Formatted note and place in the Lesson
After the raw save, enrichment SHALL also return a short `title` and a `formatted_body` produced from the transcript with Course, Lesson and Slide context: dictation errors fixed, the Course's terms, bullets, formula or code where the subject calls for it, in the Learner's language, adding no fact absent from the transcript or Slide. The raw text MUST stay stored and viewable ("Show original") and MUST NOT be overwritten. A failed formatting MUST leave the raw note (a Voice note returns to `raw_only`) and give a free turn back. Formatting of a Voice note SHALL be free for 5 turns per Learner per local day, counted and enforced by the API in Postgres on the daily-cap clock; once used, the note MUST be saved raw and MUST NOT be formatted or charged automatically, and an explicit Format request SHALL charge the existing `quick_note` price with no price change. The Notes page SHALL show Course, Lesson and Slide for an anchored note and a timestamp chip that opens the Lesson at that Slide and time. `title` and `formatted_body` MUST follow the note's backup, export, retention and erasure and MUST be blanked by the backup anonymiser.

#### Scenario: Formatted voice note
- **WHEN** a voice note on a chemistry Slide is enriched
- **THEN** the note shows a short title and a formatted body using the Course's terms, and "Show original" shows the raw transcript

#### Scenario: Open from the chip
- **WHEN** the Learner taps the timestamp chip
- **THEN** the Lesson opens on that Slide at that time

#### Scenario: Formatting fails
- **WHEN** formatting times out or fails a check
- **THEN** the raw note is kept, no Credits are charged and retry is offered

#### Scenario: Free formats
- **WHEN** a Learner saves a Voice note and has free turns left
- **THEN** the note is formatted at no charge and the rail icon badge shows the turns left (5 to 1, hidden at 0)

#### Scenario: Past the free formats
- **WHEN** a Learner saves a Voice note with 0 free turns left
- **THEN** the note is saved raw, nothing is formatted or charged, and the Saved toast offers one action, Format, at the existing `quick_note` price

#### Scenario: One turn per note
- **WHEN** the same Voice note is saved again with the same action key, or two saves race for the last turn
- **THEN** at most one turn is used per note and the daily count never passes 5
