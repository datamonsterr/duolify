## ADDED Requirements

### Requirement: Socratic hint ladder and answer guard
When a Tutor chat turn relates to a test, retry or review question the Learner has not yet answered correctly, the tutor SHALL respond with a hint ladder: a nudge, then a guiding sub-question, then one worked step, at most 3 turns before the worked step. It MUST NOT state the correct choice. The Engine SHALL check each reply deterministically for the correct choice text; on a match it SHALL regenerate once and then use a fixed hint template.

#### Scenario: Learner asks for the answer
- **WHEN** a Learner asks "what is the answer to question 2?" during the test
- **THEN** the reply is a hint and does not contain the correct choice text

### Requirement: Teach it back
After a Lesson, the client SHALL offer an optional "Teach it back" mode. The Course Character, or the default guide, plays a confused novice and asks at most 2 follow-up questions about the Learner's explanation. A grader summary then lists the key points covered and missed. The mode is platform-paid, at most once per Lesson, and never gates progress. Persona data stays style-only under the existing persona boundary.

#### Scenario: Missing key point
- **WHEN** the Learner's explanation omits one of the Lesson key points
- **THEN** the summary names that point and links to its Slide
