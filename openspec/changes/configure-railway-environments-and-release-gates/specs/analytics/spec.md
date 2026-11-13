## ADDED Requirements

### Requirement: Environment plan validation telemetry
The analytics catalogue SHALL include `environment_plan_validated` with `environment` and `ok`.
Events MUST carry the reserved operations UUID `00000000-0000-4000-8000-000000000011` and common
properties (`source=api`, `platform=web`, `app_version`), honour opt-out, and strictly exclude
private text and secrets.

#### Scenario: Environment plan evaluated
- **WHEN** an environment plan is evaluated
- **THEN** `environment_plan_validated` is emitted with environment and outcome properties
