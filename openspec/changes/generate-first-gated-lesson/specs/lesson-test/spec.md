## MODIFIED Requirements

### Requirement: Test content
Each generated Lesson SHALL include a Lesson test of multiple-choice Questions, each with a prompt, choices, one correct answer and an explanation. The Learner MUST NOT receive `answer_index` or explanation before submitting an answer to that Question. Until E4-02 adds explicit engine topic fields, the narrow tracer maps each validated original question to one API-owned topic.

#### Scenario: Test loaded
- **WHEN** the client fetches a Lesson test
- **THEN** Questions include prompt and choices but not the correct answer or explanation

### Requirement: Grading and pass rule
The API SHALL grade submissions server-side. The Learner MUST pass the Lesson test only by resolving every topic correctly. A pass SHALL be the only event that marks the Lesson completed and enqueues completion to the outbox. Reaching the last slide of a lesson MUST NOT mark the lesson complete or bypass grading.

#### Scenario: Pass
- **WHEN** every topic of the Lesson test has a correct answer
- **THEN** the Lesson stub status is marked completed, the completion is committed to the outbox once, and the next Lesson is marked ready_to_generate

#### Scenario: Wrong answer
- **WHEN** a Learner submits a wrong choice
- **THEN** the response reveals the correct answer and explanation, and the question is queued as a safe reordered-source fallback

## ADDED Requirements

### Requirement: Gated generation and reservation
Only eligible lessons (first lesson or predecessor passed) SHALL trigger lesson generation. Generation SHALL run as an idempotent River job with version-pinned reservation and SSE streaming. Malformed slides, questions, or narration MUST reject delivery and terminal failure MUST release reserved credits.

#### Scenario: Terminal failure releases credits
- **WHEN** generation fails after all attempts
- **THEN** the job is marked failed, reserved credits are released, and the lesson stays ungenerated
