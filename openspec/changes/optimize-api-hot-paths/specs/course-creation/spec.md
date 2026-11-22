## ADDED Requirements

### Requirement: Backed-off creation event polling
The course-creation event stream SHALL read creation state at 200 ms after it opens or the state changes, doubling the interval up to 3 seconds while the state is unchanged. Heartbeats and the terminal `enrolled` and `failed` events SHALL be unchanged.

#### Scenario: Idle generation
- **WHEN** a creation stays in one state for a minute
- **THEN** the stream reads it fewer than 40 times and still sends heartbeats
