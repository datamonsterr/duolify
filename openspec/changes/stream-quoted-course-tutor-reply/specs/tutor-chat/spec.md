## ADDED Requirements

### Requirement: Quoted course-level tutor turn
A Course-page tutor turn SHALL show the short or long class, daily and paid balance and a versioned quote before sending. The API MUST reserve Credits before any Engine call and MUST refuse a turn the eligible balance cannot cover without calling the Engine, persisting anything or discarding the typed draft. Context MUST contain the Course outline, the effective Learner profile (empty when unset) and oldest-first completed-Lesson history; `current_lesson` MUST be empty at Course level. Completed usage SHALL settle exactly once.

#### Scenario: Paid course turn
- **WHEN** a Learner sends a message with an accepted fresh quote
- **THEN** one reply streams over SSE, the thread and both messages persist and the charge commits in the same transaction

#### Scenario: Insufficient credits
- **WHEN** eligible credits are lower than the quote
- **THEN** the API refuses before the Engine call, nothing is persisted and the client keeps the draft and shows the upsell

### Requirement: Tutor thread privacy and isolation
Threads and messages MUST be readable and deletable only by their owner; another Learner's identifiers MUST return not-found. Context sent to the Engine MUST never include another Learner's data. Learner text and Course content MUST be treated as untrusted data: delimited, escaped and never concatenated into instructions.

#### Scenario: Foreign thread
- **WHEN** a Learner requests another Learner's thread or messages
- **THEN** the API returns 404 without content

#### Scenario: Injection attempt
- **WHEN** a message or outline title contains instructions to ignore the rules
- **THEN** it appears only as escaped data and the tutor keeps the course scope

### Requirement: Interrupted and duplicate turns
After the first delta a failure SHALL end the reply with an error event and persist exactly one assistant message holding the delivered prefix, committed atomically with settlement of the delivered usage; no fallback is spliced after a delta and no second persona reply is produced. With zero delivered content no message is stored and the reservation is fully released. Concurrent sends against one wallet or thread MUST NOT double-charge or exceed the balance, and a retry with the same idempotency key MUST NOT duplicate messages or charges.

#### Scenario: Mid-stream failure
- **WHEN** the Engine fails after some deltas
- **THEN** one interrupted assistant message with the prefix is stored and partial usage settles

#### Scenario: Retry after interrupt
- **WHEN** the client resends the same idempotency key
- **THEN** the stored message is replayed and no Engine call or charge occurs

### Requirement: Tutor analytics and data handling
The API SHALL emit `tutor_message_sent`, `tutor_reply_completed` and `tutor_reply_failed` with identifiers, class, latency and token counts only, honouring opt-out, and settlement SHALL emit `$ai_generation`. Threads and messages are covered by nightly dumps, per-thread deletion and account erasure; message text MUST NOT reach PostHog or logs. Golden-set `tutor_chat` cases MUST cover grounding, profile, history, empty profile and prompt injection with critical checks.

#### Scenario: Completed reply
- **WHEN** a reply completes
- **THEN** `tutor_reply_completed` carries `course_id`, `class`, `model`, `latency_ms`, `input_tokens`, `output_tokens` and no text
