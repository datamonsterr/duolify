## MODIFIED Requirements

### Requirement: Course data backup and retention
Course, Unit, Lesson stub, enrollment and generated Lesson rows MUST live in Postgres and be covered by the nightly dump. Slide HTML MUST live in object storage with versioning. Deleting a Course MUST soft-delete first and hard-delete after 30 days with unreferenced creation and asset record cleanup.

#### Scenario: Owner deletes course
- **WHEN** the authenticated owner deletes a Course
- **THEN** `deleted_at` is set, the Course is immediately hidden from course listings and foreign requests return 404
- **AND** if active, `active_course_id` is cleared

#### Scenario: Course purge after 30 days
- **WHEN** the purge job runs on courses soft-deleted older than 30 days
- **THEN** the Course, Units, Stubs, Enrollments and unreferenced Creations are purged from Postgres

### Requirement: Generation is a background job
Generation SHALL run as a River job, never on the request path. A job MUST be idempotent per Lesson: a retry MUST NOT create a second Lesson or charge Credits twice. Outline creation jobs MUST support authenticated SSE reconnection, automatic terminal failure recovery on deadline expiry, and retry only with fresh quote consent. River cancellation, worker restart, and duplicate job deliveries MUST not produce duplicate enrollments or double charges.

#### Scenario: Creation worker restarts or disconnects
- **WHEN** a client disconnects or worker restarts during course creation
- **THEN** the client reconnects to the existing job via SSE without charging again
- **AND** if the job terminated or timed out, reserved credits are released and explicit retry is offered with a fresh quote

## ADDED Requirements

### Requirement: Creation flow and outline recovery
The system SHALL preserve confirmed briefs, working manual outline edits, undo history, and applied proposal diffs across page reloads, job retries, and returns from the credits shop. The client SHALL restore the furthest authorized step in the Brief → Preview → Outline → Create funnel. Stale proposals or failed suggestion requests MUST leave the existing outline intact and MUST NOT silently resubmit chat edits or charge on reconnect.

#### Scenario: Return from credits shop
- **WHEN** a learner navigates to purchase credits and returns to course creation
- **THEN** the confirmed brief, manual outline edits, and creation step are restored without data loss

#### Scenario: Stale proposal recovery
- **WHEN** an outline proposal failed or timed out during disconnect
- **THEN** the outline remains intact, reserved proposal credits are released, and no proposal changes are silently applied
