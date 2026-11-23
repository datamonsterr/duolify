## MODIFIED Requirements

### Requirement: Streaming replies
The API SHALL continue the existing SSE path using `ChatEvent` deltas. With `CHAT_EDGE_ENABLED=true`, the client SHALL prefer a hibernating WebSocket to an authenticated Durable Object per Learner session, use Edge SSE when WebSocket transport fails, and use the existing Go/Engine path when the edge route is unavailable. A switch MUST resolve the existing turn before any retry. The Learner MUST be able to stop a reply and settle Credits for delivered usage under the existing rules.

#### Scenario: Learner stops a reply
- **WHEN** the Learner taps stop on any transport
- **THEN** the active turn ends and its reservation settles once for delivered usage

#### Scenario: Edge registration disabled
- **WHEN** the server runs with default `CHAT_EDGE_ENABLED=false`
- **THEN** the client uses the existing Go/Engine path and Edge chat cannot start new turns

#### Scenario: WebSocket upgrade fails
- **WHEN** the approved WebSocket cannot upgrade and the Learner has an eligible turn
- **THEN** the client uses Edge SSE with the same turn identity and can fall back to the existing Engine path under the same idempotency rules

### Requirement: Quoted course-level tutor turn
A Course-page tutor turn SHALL show the short or long class, daily and paid balance and versioned quote before sending. Go MUST reserve before any Engine call and refuse an uncovered turn without invoking the Engine or discarding the draft. Context MUST contain the Course outline, effective Learner profile (empty when unset) and oldest-first completed-Lesson history, with `current_lesson` empty at Course level. Go MUST persist and settle each delivered turn exactly once regardless of transport.

#### Scenario: Paid course turn
- **WHEN** a Learner sends a message with an accepted fresh quote
- **THEN** one reply streams over the selected transport and the thread, messages and charge commit in one Go transaction

#### Scenario: Insufficient credits
- **WHEN** eligible Credits are lower than the quote
- **THEN** the API refuses before the Engine call, nothing is persisted and the client keeps the draft and shows the upsell

## ADDED Requirements

### Requirement: Edge chat authority and recovery
The Worker MUST validate a Go-authorized session and thread scope before connecting or resuming an Edge chat. The Durable Object SHALL authenticate to Go using scoped service credentials and delegate quote, reserve, settle, release, chat history and cancellation to the existing API authority. Postgres MUST remain the source of truth for chat history and turn state, protected by the existing encrypted backup, export and deletion policies. Edge connection state MUST be disposable and MUST exclude durable chat content or ledger state. Recovery after hibernation or transport loss MUST resolve the existing turn and MUST NOT replay inference after a delivered delta or duplicate a charge.

#### Scenario: Resume over a different transport
- **WHEN** the same authorized session reconnects with a stable turn id after hibernation or a transport failure
- **THEN** it recovers the canonical outcome or existing interruption state and no duplicate inference, transcript row or credit settlement occurs

#### Scenario: Another session or revoked thread
- **WHEN** a client presents an unauthorized or revoked session or references another Learner's thread
- **THEN** the Worker and Go refuse access before returning any messages or admitting a turn

#### Scenario: Restore without Edge state
- **WHEN** a restore recovers Postgres and deletion records while Edge connection state is empty
- **THEN** authorized sessions see the same retained history and deleted threads remain unavailable
