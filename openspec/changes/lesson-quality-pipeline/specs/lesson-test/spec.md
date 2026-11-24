## ADDED Requirements

### Requirement: Non-gating review questions
The API SHALL add up to 2 review questions to each Lesson test from stored question variants of earlier Lessons. Candidates are restricted to the current Lesson plan's review targets when the plan has them, otherwise to any earlier passed Lesson. The weakest topics come first: most wrong answers, then the oldest pass. Review questions SHALL be marked `review`, MUST NOT count toward passing the test, MUST NOT trigger retry variants or recaps, and SHALL be recorded with their result. No generation call and no Credits are used.

#### Scenario: Review question answered wrong
- **WHEN** a Learner answers a review question wrong
- **THEN** the explanation is shown and the result is recorded as a weak signal for that topic
- **AND** the test pass state is unchanged

#### Scenario: First Lesson
- **WHEN** the Lesson has no earlier passed Lesson
- **THEN** no review questions are added
