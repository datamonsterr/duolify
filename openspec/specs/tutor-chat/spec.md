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
Each chat turn MUST reserve and settle Credits as defined in the `credits` spec, drawing from Daily free credits first. The client MUST show the remaining Daily free credits, the balance and the estimated cost class before sending. When the Daily free credits and all other Credits are exhausted, the turn MUST be refused before any Engine call, the typed message MUST be kept as a draft, and the client MUST show the upsell screen with the next Daily free credits time and the top-up options. A refused turn MUST NOT fail silently.

#### Scenario: Insufficient credits
- **WHEN** the spendable balance is lower than the estimate
- **THEN** the turn is refused, no Engine call is made, the draft is kept and the upsell screen is shown

#### Scenario: Daily allowance used first
- **WHEN** a Learner has Daily free credits and purchased Credits
- **THEN** the turn is charged to the Daily free credits first

#### Scenario: Allowance covers typical use
- **WHEN** a Learner with only the daily grant sends 5 long, context-heavy messages
- **THEN** all 5 succeed

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

### Requirement: Character Tutor chat persona
Tutor chat SHALL use the main guide as its default persona and allow the Learner to choose a supporting Character from the effective Cast. Inside a Lesson the default Cast MUST be its pinned Lesson snapshot; outside it MUST be the Course's current approved Cast, or the Default cast when none is selected. The API MUST pin the Character version for each turn. The Engine MUST receive validated persona/info only as a bounded escaped data section below system policy, together with the existing Course, Lesson, profile and history context. Persona style MUST NOT override grounding, safety, tools, output rules, grading or Credits.

#### Scenario: Guide answers in character
- **WHEN** a Learner asks a Course question without choosing a different tutor
- **THEN** the reply uses the effective main guide's speaking style while staying grounded in the Course

#### Scenario: Persona changes between turns
- **WHEN** a Learner changes the tutor after a reply has started
- **THEN** the active turn keeps its pinned Character and the next turn uses the new choice without rewriting messages

#### Scenario: Injection in persona
- **WHEN** a selected persona asks to reveal system instructions or bypass credit checks
- **THEN** the tutor does neither and the normal safety/credit rules remain enforced

### Requirement: Tutor persona selection and pinning
The send request MAY carry `speaker_key` (`guide`, `supporter`, `supporter_2`; default `guide`). The API MUST resolve the Cast from the Learner's own Course/Lesson rows, store the effective Character id, version, speaker key, Cast source and any fallback code on the turn before calling the Engine, send only the bounded persona JSON in `tutor_persona_json`, and announce the pinned tutor with a `tutor` SSE event after `thread`. `GET /v1/courses/{id}/tutor/personas` lists the effective tutors (optionally for a Lesson).

#### Scenario: Selection during streaming
- **WHEN** the Learner or Course Cast changes while a reply streams
- **THEN** the streaming turn keeps its pinned Character and only the next turn uses the change

### Requirement: Tutor persona analytics events
The system SHALL emit PostHog `tutor_character_selected` (speaker_key, character_version, cast_source) and `tutor_character_fallback` (reason_code, phase), alongside existing tutor events and `$ai_generation` usage. Events MUST follow the `analytics` delta, respect opt-out and contain no Character info, persona, message or prompt text.

#### Scenario: Persona fallback recorded
- **WHEN** an invalid persona is replaced before a reply
- **THEN** `tutor_character_fallback` has `phase` `before_stream` and a bounded reason code

### Requirement: Tutor Character data backup and retention
Each chat turn's effective Character/version and immutable spec reference MUST be stored with the thread in Postgres and included in nightly dumps. Referenced S3 rig assets MUST follow versioned nightly asset backup/restore under `custom-characters`; no new tutor audio asset is required in v1. Thread deletion MUST remove turn references and unshared retained persona snapshots, and account deletion MUST remove them within 30 days. Shared Lesson/published references MUST retain only their independently required content under their existing policies.

#### Scenario: Thread deleted
- **WHEN** a Learner deletes a thread
- **THEN** its messages and per-turn Character references leave later context and cannot keep an otherwise unreferenced private spec alive

### Requirement: Tutor persona model testing and reliability
Golden set `character_persona_chat` and `character_injection` cases MUST cover adherence, grounding, refusals, hidden-prompt protection and version consistency; they MUST meet `custom-characters` thresholds in `evals/thresholds.toml` and pass `mise run engine:eval`. Invalid/unavailable persona context MUST fall back to an approved Default cast persona before streaming, within existing first-token/idle timeouts and turn token/spend caps. Model failures MUST use existing provider fallback before the first delta; mid-stream failures MUST end once with an error and release unused Credits, without restarting in another persona. Streaming outputs MUST retain the existing safety checks.

#### Scenario: Safe default before streaming
- **WHEN** a custom tutor spec fails validation at turn start
- **THEN** one safe default reply is attempted within the existing turn ceiling and no malformed persona reaches the Engine

#### Scenario: Mid-stream failure
- **WHEN** a provider fails after sending deltas in a Character's voice
- **THEN** the reply ends with the existing retry option and no second persona reply is spliced into it

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

### Requirement: Stop and recover a streamed reply
The Learner MUST be able to stop a reply from the moment it is sent until it ends. Stop, client disconnect and Engine failure SHALL each end the Engine stream, store exactly one assistant message holding the delivered prefix (`stopped` for an explicit stop, otherwise `interrupted`) and settle the delivered usage exactly once in the same transaction; with no delivered content nothing is stored and the whole reservation is released. Stop MUST be owner-scoped and idempotent; stopping after completion or a second time SHALL be a no-op and MUST NOT charge or settle again. The owner MUST be able to read the stored partial of a turn after a dropped connection; any other Learner MUST get not-found.

#### Scenario: Learner stops mid-reply
- **WHEN** the Learner presses Stop after some deltas
- **THEN** the stream ends, one `stopped` message with the prefix is stored and only produced usage is charged

#### Scenario: Foreign stop or read
- **WHEN** another Learner stops or reads the turn
- **THEN** the API returns 404, the stream continues and nothing changes

#### Scenario: Stop races completion
- **WHEN** Stop arrives as the reply completes
- **THEN** the turn settles once, as completed if the stream had finished, otherwise as stopped

#### Scenario: Retry after interruption
- **WHEN** the Learner asks again after a stop or interruption
- **THEN** a fresh quote and key are used and the first turn is never replayed or charged again

### Requirement: Typed streaming errors
The Engine MUST report first-token, idle and deadline timeouts and provider failures as typed stream errors. A first-token timeout or failure before any delta MAY use one evaluated fallback within the quote caps; after the first delta no second provider reply is started. An idle timeout SHALL end the reply with a typed, retryable error.

#### Scenario: Idle timeout
- **WHEN** no delta arrives within the idle timeout after output started
- **THEN** the SSE `error` event has `code=idle_timeout`, `retryable=true`, the prefix is stored as interrupted and the client offers Ask again

### Requirement: Full-course tutor context within limits
The tutor context MUST be assembled statelessly per turn from the authenticated Learner's own data for the Course: outline, every lesson summary, the effective profile, recent messages and a bounded set of source excerpts retrieved from older generated lessons that match the question. Retrieval and summaries MUST NOT include another Learner's or another Course's content, deleted threads or deleted lessons. Retrieval queries MUST be parameterized. Summaries MUST NOT be dropped silently; the prompt MUST stay within the quote's input bound, which stays under the configured prompt-token limit. When the mandatory parts exceed the standard bound but fit the limit, the API MUST answer a typed `context_approval_required` and only an explicit Learner approval bound to a new quote MAY proceed; above the limit it MUST answer `context_too_large`. Neither case charges Credits.

#### Scenario: Ask about the oldest lesson
- **WHEN** a Learner asks about a topic taught only in the first lesson of a large Course
- **THEN** the prompt contains that lesson's excerpt and every summary and the reply is grounded in it

#### Scenario: Oversized Course
- **WHEN** all summaries together exceed the standard bound
- **THEN** the oldest summaries are kept, the turn does not run, nothing is charged and the response is the typed approval or too-large answer

#### Scenario: Approved large context
- **WHEN** the Learner approves and the quote is created with `approve_large_context`
- **THEN** the send runs under the larger Pricing-derived bound at the same credit price

#### Scenario: Injection in an old lesson
- **WHEN** a retrieved lesson says to ignore instructions
- **THEN** it stays inside the untrusted data block and the tutor keeps grounding, refusal and safety behaviour

#### Scenario: Foreign or deleted content
- **WHEN** another Learner's lesson, another Course's lesson or a deleted lesson matches the question
- **THEN** it is never retrieved or summarised

### Requirement: Lesson tutor sheet with authorized current lesson context
The lesson player SHALL open the tutor in the shared responsive panel. Opening it MUST pause playback; closing it MUST restore focus to the opener and the same slide and playback time exactly once. The client MUST send only a lesson id and slide index; the API MUST verify that the lesson belongs to the authenticated Learner and to the Course in the URL, MUST build `current_lesson` from its own rows, and MUST answer 404 `not_found` for another Learner's, another Course's or an unknown lesson without charging, storing or calling the Engine. Another Learner's data MUST NOT enter the context. Learner text stays untrusted data in the prompt.

#### Scenario: Ask about the current lesson
- **WHEN** the Learner opens the sheet on a slide and sends a question
- **THEN** playback is paused, the prompt carries that lesson's title and goal, and closing the sheet returns focus, slide and time

#### Scenario: Foreign lesson id
- **WHEN** the request names a lesson of another Learner or another Course
- **THEN** the answer is 404 and no credit, message or Engine call results

### Requirement: Accessible streaming and draft recovery in the lesson sheet
Stop MUST be reachable by keyboard. The sheet MUST NOT announce tokens; it MUST announce only the start and the finish of a reply. Offline or with no eligible Credits the unsent draft MUST be kept, the next reset or top-up shown, and nothing resent automatically. Local drafts MUST be scoped to the signed-in Learner and cleared on logout.

#### Scenario: Zero credits
- **WHEN** the send is refused for credits
- **THEN** the draft stays in the composer, the next grant time and top-up link show, and no request repeats until the Learner acts

#### Scenario: Logout
- **WHEN** the Learner signs out
- **THEN** every stored tutor draft is removed and the next Learner on the device sees none

### Requirement: Save a reply as one raw Quick note
The Learner SHALL save a complete or stopped assistant reply from their own thread as exactly one raw Quick note with message and thread provenance and an optional authorized lesson link. Repeating the request MUST return the same note. Saving MUST NOT require Credits. Enrichment MUST stay a separate, separately quoted action owned by `quick-notes`. A reply longer than 2,000 characters MUST be truncated at that bound and say so. Another Learner's message, a user message or an unknown id MUST answer 404 and create nothing.

#### Scenario: Save twice
- **WHEN** the Learner saves the same reply twice, including after a lost response
- **THEN** one note exists and both answers carry its id

#### Scenario: Zero Credits
- **WHEN** the Learner has no Credits
- **THEN** the raw note is saved with status `enrichment_pending`

### Requirement: Delete a thread everywhere
Deleting a thread MUST be owner-only (404 otherwise) and in one transaction remove its messages and every context reference, record a text-free tombstone, and mark notes saved from it `source unavailable`. Later tutor turns MUST NOT include that history. Notes saved from it MUST stay readable. Restoring a backup MUST re-apply the tombstone before serving.

#### Scenario: Later turn after deletion
- **WHEN** a thread is deleted and the Learner asks again
- **THEN** the Engine request contains none of the deleted messages

#### Scenario: Another Learner
- **WHEN** a different Learner deletes the thread id
- **THEN** the answer is 404 and nothing changes

#### Scenario: Restore
- **WHEN** a backup taken before the deletion is restored
- **THEN** the thread and messages are removed again and the note shows source unavailable
