## MODIFIED Requirements

### Requirement: Test result feeds history
The API SHALL persist a short summary of each completed Lesson and its test result with weak topics (missed questions). These summaries MUST be sent as `history` to the Engine for subsequent Lesson generation, oldest first.

#### Scenario: Next lesson generation
- **WHEN** Lesson N+1 is generated
- **THEN** the request history contains summaries of completed Lessons, oldest first, including weak topics from missed Questions

### Requirement: Gated generation and reservation
Only eligible lessons SHALL trigger lesson generation. For any lesson beyond the first, the predecessor Lesson MUST have a recorded authoritative pass; missing predecessor pass MUST refuse before any Engine call. Passing Lesson N enqueues only Lesson N+1. A failed or unaffordable next generation MUST NOT undo an already completed Lesson.

#### Scenario: Predecessor pass required
- **WHEN** preparation of Lesson N+1 is requested and Lesson N is not completed
- **THEN** the API refuses with a 409 conflict error and no Engine call or charge occurs

#### Scenario: Next generation failure leaves completion intact
- **WHEN** preparation of Lesson N+1 fails or credits are insufficient
- **THEN** reserved credits are released and Lesson N remains completed

#### Scenario: Duplicate pass submission
- **WHEN** the same passing attempt is sent twice
- **THEN** the completion outbox exists once and no duplicate generation job or charge is created
