## ADDED Requirements

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
