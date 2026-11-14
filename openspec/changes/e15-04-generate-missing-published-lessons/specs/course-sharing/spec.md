## ADDED Requirements

### Requirement: Generate the next missing Lesson in a Course copy
At the end of the published content of a Course copy, the API SHALL show how many Lessons are ready out of the total and an explicit quote at the current Pricing config version before generating the next missing Lesson. Generation MUST require that the previous Lesson test is passed and that the Learner gave explicit consent to that quote. The generated Lesson MUST be stored only in the Learner's copy; the published snapshot, its content hash and every other copy MUST stay unchanged. The generating Learner pays; the owner receives nothing and no update is forked.

#### Scenario: Quote then generate
- **WHEN** a Learner passed the last published Lesson, sees the quote and consents
- **THEN** the next Lesson is generated through the gated pipeline and charged at the quoted version

#### Scenario: Previous Lesson not passed
- **WHEN** a Learner requests a quote or generation before passing the previous Lesson
- **THEN** the API refuses with `previous_lesson_not_passed` and the Engine is not called

#### Scenario: No consent or stale quote
- **WHEN** consent is missing, or the quote expired or its Pricing config version is no longer active
- **THEN** the API refuses with `consent_required` or `quote_stale`, charges nothing and the Learner can request a new quote

#### Scenario: Not enough Credits
- **WHEN** the balance is below the quoted content and audio ceilings
- **THEN** the API refuses with `insufficient_credits` before any Engine call

#### Scenario: Failure
- **WHEN** generation fails after reservation
- **THEN** undelivered components are released, earlier Lessons stay available and the Learner can retry

#### Scenario: Owner content untouched
- **WHEN** a Learner generates a Lesson beyond the snapshot
- **THEN** the snapshot, content hash, owner Course and other copies are byte-identical
