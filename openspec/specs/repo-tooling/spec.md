# repo-tooling Specification

## Purpose
Keep repository tooling uniform: every area exposes the same mise tasks, CI calls only those tasks and ends in one required check, and contract changes land in both services together.

## Requirements

### Requirement: Uniform area tasks
Every area (api, engine, client, proto, infra) SHALL expose `<area>:lint`, `<area>:test` and `<area>:build` as mise tasks defined in `.mise/tasks/<area>.toml`. CI MUST call only mise tasks.

#### Scenario: Task listing
- **WHEN** a developer runs `mise tasks`
- **THEN** each area shows its lint, test and build tasks

### Requirement: Single required check
The CI workflows SHALL end in one aggregate job `ci-ok`, and `ci-ok` MUST be the only required status check. Mobile device jobs MAY initially be advisory and excluded from the aggregate; Android SHALL be considered for promotion after three consecutive green runs.

#### Scenario: Required job added
- **WHEN** a required CI job is added
- **THEN** `ci-ok` depends on it and the required check name does not change

#### Scenario: Advisory mobile checks
- **WHEN** an advisory mobile device job fails
- **THEN** the failure is visible without failing the workflow or preventing publication and deploy jobs

### Requirement: Contract changes in both services
A change to `proto/` MUST include matching changes in `services/api` and `engine` in the same pull request.

#### Scenario: Proto only change
- **WHEN** a pull request changes the proto and one side only
- **THEN** CI fails on the other side's build
