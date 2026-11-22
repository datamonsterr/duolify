## MODIFIED Requirements

### Requirement: Output validation
The Engine MUST validate every AI output against schema and safety checks before returning it. Every JSON-returning task MUST request structured output from the provider when the model supports it (`response_format`, routed only to endpoints that honour it) and MUST pass the answer through one shared extraction rule: surrounding whitespace and a single surrounding code fence are removed, nothing else. An answer that is not schema-valid after extraction MUST be rejected; prose, partial JSON and unknown enum values MUST NOT be interpreted. A rejected answer MUST be followed, within the attempt limit, by one corrective repair that quotes the validation error, and if still invalid the Engine returns an error. Safety screens MUST stay fail closed. Unsafe Slide HTML MUST never be returned.

#### Scenario: Invalid JSON from model
- **WHEN** the model returns malformed structured output
- **THEN** the Engine retries once with a repair turn quoting the validation error and, if still invalid, returns an error

#### Scenario: Fenced JSON
- **WHEN** the model wraps a valid JSON answer in one ```` ```json ```` fence
- **THEN** the Engine strips the fence, validates the JSON and delivers the unfenced JSON

#### Scenario: Prose safety verdict
- **WHEN** a screen answers `**verdict:** pass` or `allowed: true`
- **THEN** the answer is invalid, the repair attempt runs, and if no schema-valid verdict arrives the screen fails closed

#### Scenario: Unknown reason code
- **WHEN** a screen answers a code outside its closed set, such as `secrets`
- **THEN** the answer is invalid and the repair turn names the allowed codes; the code is never mapped silently

## ADDED Requirements

### Requirement: Live golden fidelity
Live golden cases for non-screen tasks MUST run the production prompt builders, validators and bounded runner on production request data, not dataset text sent verbatim. Screen cases MUST run the production screen functions.

#### Scenario: Live lesson case
- **WHEN** the live eval runs a lesson case
- **THEN** it builds the production lesson prompt from the case's outline, lesson and learner data and scores whether the production validator delivered the lesson
