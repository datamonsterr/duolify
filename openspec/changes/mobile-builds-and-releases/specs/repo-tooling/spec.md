## MODIFIED Requirements

### Requirement: Single required check
The CI workflows SHALL end in one aggregate job `ci-ok`, and `ci-ok` MUST be the only required status check. Mobile device jobs MAY initially be advisory and excluded from the aggregate; Android SHALL be considered for promotion after three consecutive green runs.

#### Scenario: Required job added
- **WHEN** a required CI job is added
- **THEN** `ci-ok` depends on it and the required check name does not change

#### Scenario: Advisory mobile checks
- **WHEN** an advisory mobile device job fails
- **THEN** the failure is visible without failing the workflow or preventing publication and deploy jobs
