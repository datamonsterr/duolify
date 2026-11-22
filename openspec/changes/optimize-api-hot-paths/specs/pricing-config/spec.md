## ADDED Requirements

### Requirement: Bounded-freshness catalogue snapshot
`GET /v1/pricing` SHALL serve an in-memory snapshot of the active catalogue that is at most 5 seconds older than the last successful Postgres read, and SHALL run no SQL for a request inside that bound. A process that activates a version SHALL serve it immediately; another replica SHALL serve it within the bound after the activation marker exists, so a scheduled flip is served everywhere within the activator interval plus the bound. A failed reload SHALL keep serving the last validated catalogue, raise the existing alert and be retried no more than once per bound. The response SHALL carry an ETag and `Cache-Control: public, max-age=10`.

#### Scenario: Warm request
- **WHEN** the snapshot is inside its bound
- **THEN** requests, including conditional ones, run no database statement

#### Scenario: Reload failure
- **WHEN** the database is unavailable after the snapshot expires
- **THEN** the last good catalogue is served, the alert is raised, and the database is queried at most once per bound
