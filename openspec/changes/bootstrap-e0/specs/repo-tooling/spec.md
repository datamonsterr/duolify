## ADDED Requirements

### Requirement: Uniform area tasks
Every area (api, engine, client, proto, infra) SHALL expose `<area>:lint`, `<area>:test` and `<area>:build` as mise tasks defined in `.mise/tasks/<area>.toml`. CI MUST call only mise tasks.

#### Scenario: Task listing
- **WHEN** a developer runs `mise tasks`
- **THEN** each area shows its lint, test and build tasks

### Requirement: Single required check
The CI workflows SHALL end in one aggregate job `ci-ok`, and `ci-ok` MUST be the only required status check.

#### Scenario: Job added
- **WHEN** a CI job is added
- **THEN** `ci-ok` depends on it and the required check name does not change

### Requirement: Contract changes in both services
A change to `proto/` MUST include matching changes in `services/api` and `engine` in the same pull request.

#### Scenario: Proto only change
- **WHEN** a pull request changes the proto and one side only
- **THEN** CI fails on the other side's build
