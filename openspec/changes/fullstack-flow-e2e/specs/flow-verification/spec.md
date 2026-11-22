## ADDED Requirements

### Requirement: Full-stack flow suite
The project SHALL have a browser flow suite that drives the real web client against a running
stack (web, API, worker, engine, Postgres, Redis, object storage) without intercepting requests
to its own API. The stack URL MUST be configurable so the same suite runs against a local
disposable stack and a remote environment.

#### Scenario: Deterministic local run
- **WHEN** a developer runs `mise run client:e2e-stack-up` and `mise run client:e2e-stack`
- **THEN** the suite runs against the local stack with the mock engine provider and no paid calls

#### Scenario: Live provider run
- **WHEN** `E2E_LIVE=1` and an OpenRouter key are set
- **THEN** the engine uses the live OpenRouter route for the same flows

#### Scenario: Remote run
- **WHEN** `E2E_STACK_URL` points at a deployed environment
- **THEN** the suite runs against it without starting a local stack

### Requirement: Reviewable flow evidence
Every flow SHALL save a named full-page screenshot at each meaningful step under
`<flow-id>/<nn>-<step>.png`, and each run SHALL write a `flows.json` manifest listing every
flow's id, title, steps, screenshot paths and outcome. CI MUST upload the folder.

#### Scenario: Gated flow
- **WHEN** a flow depends on a feature that is disabled in the stack under test
- **THEN** the flow is reported as `fixme` with the reason, not as passed
