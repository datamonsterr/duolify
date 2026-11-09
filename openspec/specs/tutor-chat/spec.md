# tutor-chat Specification

## Purpose
Give the Learner a chatbot that knows the whole Course, the current Lesson, the Learner profile and the learning history. Replies stream live and cost Credits.

## Requirements

### Requirement: Context assembly
The API SHALL build each Tutor chat request with the Course outline, the current Lesson (when the Learner is in one), the Learner profile, Lesson summaries as history, and recent messages. The Engine MUST be stateless and MUST receive all context in the `ChatRequest`.

#### Scenario: Chat inside a Lesson
- **WHEN** a Learner asks a question during a Lesson
- **THEN** the request contains the current Lesson, the Course outline and history

#### Scenario: Chat outside a Lesson
- **WHEN** a Learner opens chat from the Course page
- **THEN** `current_lesson` is empty and the Course outline and history are still sent

### Requirement: Streaming replies
The API SHALL stream replies to the client through SSE using `ChatEvent` deltas. The Learner MUST be able to stop a reply, and stopping MUST settle Credits for tokens produced so far.

#### Scenario: Learner stops a reply
- **WHEN** the Learner taps stop
- **THEN** the stream ends and the reservation settles on the usage so far

### Requirement: Credits
Each chat turn MUST reserve and settle Credits as defined in the `credits` spec. The client MUST show the balance and estimated cost class before sending.

#### Scenario: Insufficient credits
- **WHEN** the balance is lower than the estimate
- **THEN** the turn is refused and no Engine call is made

### Requirement: Grounding and scope
The tutor SHALL answer using Course and Lesson context, and MUST say when it does not know. The tutor MUST stay on the Course topic and MUST refuse unsafe requests per the safety policy in config.

#### Scenario: Off-topic request
- **WHEN** a Learner asks something unrelated and unsafe
- **THEN** the tutor declines briefly and offers to continue the Course

### Requirement: Save to notes and bookmarks
A Learner SHALL be able to save a tutor reply as a Quick note.

#### Scenario: Save reply
- **WHEN** a Learner taps save on a reply
- **THEN** a Quick note is created with a link to the Lesson and the message

### Requirement: Chat analytics events
The system SHALL emit `tutor_message_sent`, `tutor_reply_completed`, `tutor_reply_stopped` and `tutor_reply_failed`, and the Engine usage SHALL be reported as PostHog `$ai_generation` events, with properties from the `analytics` spec.

#### Scenario: Reply completed
- **WHEN** a reply finishes
- **THEN** `tutor_reply_completed` is emitted with `course_id`, `lesson_id`, `model`, `latency_ms`, `input_tokens` and `output_tokens`

### Requirement: Chat data backup and retention
Chat threads and messages MUST be stored in Postgres, covered by the nightly dump, and deletable by the Learner per thread. Messages MUST be deleted with the account within 30 days. Message content MUST NOT be sent to PostHog.

#### Scenario: Learner deletes thread
- **WHEN** a Learner deletes a thread
- **THEN** its messages are removed and excluded from later context

### Requirement: Chat model testing and reliability
Tutor chat MUST have Golden set cases (task `tutor_chat`) covering grounding, refusal, and use of Learner profile and history. Scores MUST meet `evals/thresholds.toml`. The Engine MUST use a first-token timeout and fall back to OpenRouter before any delta is sent. After the first delta, a failure MUST end the reply with an error event and release unused Credits.

#### Scenario: Fallback before first token
- **WHEN** the primary provider gives no token within the timeout
- **THEN** the Engine retries on the fallback provider and the Learner sees one reply

#### Scenario: Failure mid-stream
- **WHEN** the provider fails after some deltas
- **THEN** the client shows a retry option and only produced tokens are charged
