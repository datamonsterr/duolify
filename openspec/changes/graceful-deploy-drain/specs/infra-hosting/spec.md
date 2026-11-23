## ADDED Requirements

### Requirement: Rolling deploy drains before it replaces
`deploy.sh` SHALL start a new Engine and wait until it is healthy before stopping the old one, then roll each API
instance the same way, then restart the worker and proxy. Every old container MUST be stopped with a timeout
no shorter than the drain (`docker stop --timeout 120`). Compose `stop_grace_period` for api, worker and engine
SHALL be at least 15 s longer than their drain timeout, and `deploy-plan.sh` MUST reject a configuration where it is not.
A new Engine that never becomes healthy MUST fail the deploy and leave the old Engine running.

#### Scenario: Engine rolls before the API
- **WHEN** a deploy runs with a running Engine and two API instances
- **THEN** the new Engine is healthy before the old one is stopped, and the Engine is rolled before any API instance

#### Scenario: Grace period shorter than the drain
- **WHEN** `ENGINE_DRAIN_TIMEOUT_SECS` is 300 and `stop_grace_period` is 120 s
- **THEN** `deploy-plan.sh` fails naming the engine and its drain setting
