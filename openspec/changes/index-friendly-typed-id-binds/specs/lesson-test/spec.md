## ADDED Requirements
### Requirement: Index-friendly identifiers and quiet generation stream
The API SHALL compare identifier columns with typed parameters (`column = $n::uuid`) so primary-key and foreign-key indexes apply, SHALL validate identifiers at the handler boundary and answer a malformed identifier with a client error rather than a database error, and the lesson generation event stream SHALL poll with a backoff that resets on a state change while keeping its heartbeats and terminal events.

#### Scenario: Malformed lesson id
- **WHEN** a client requests a lesson, test, recap, preparation or generation route with an identifier that is not a UUID
- **THEN** the API returns 404 and runs no query against Postgres

#### Scenario: Idle generation stream
- **WHEN** a generation stays in one state for several seconds
- **THEN** the stream reads Postgres less often as time passes and still emits its heartbeat and the terminal event
