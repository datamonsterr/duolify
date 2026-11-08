## MODIFIED Requirements

### Requirement: Gated lesson generation
The system SHALL generate Lesson N+1 only after the Learner passes the Lesson test of Lesson N. The API MUST refuse to call `GenerateLesson` for a Lesson whose predecessor has no recorded pass. The `GenerateLesson` call MUST include the Learner's active Course instructions as defined in the `course-steering` capability, and the generated Lesson MUST record which instructions it used.

#### Scenario: Previous test not passed
- **WHEN** a client asks to generate a Lesson and the previous Lesson test is not passed
- **THEN** the API responds with a conflict error and the Engine is not called

#### Scenario: Previous test passed
- **WHEN** the Learner passes the Lesson test of Lesson N
- **THEN** the API enqueues a generation job for Lesson N+1
- **AND** progress is streamed to the client through SSE

#### Scenario: Active instructions
- **WHEN** the generation job for Lesson N+1 starts and the Learner has active instructions
- **THEN** the Engine call carries them and the Lesson stores their ids

## ADDED Requirements

### Requirement: Re-plan credits
Re-planning the remaining outline SHALL reserve Credits before the Engine call and settle them with the reported `Usage`, at the Pricing config version in the quote, as defined in the `credits` spec.

#### Scenario: Re-plan charged
- **WHEN** a re-plan proposal is produced
- **THEN** a `spend` entry is written with the quote's Pricing config version
