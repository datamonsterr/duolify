## ADDED Requirements

### Requirement: Authorized course enrollment reads and switching
The API SHALL expose an authorized `GET /v1/courses` endpoint returning the authenticated Learner's enrolled Courses with confirmed Units, ordered Lesson stubs with goals, and level overrides. Access to foreign or non-existent Courses MUST return a neutral not found status without leaking enrollment or existence. Active course selection SHALL be persistent in Postgres and survive client restart.

#### Scenario: Learner lists enrolled courses
- **WHEN** an authenticated Learner requests `/v1/courses`
- **THEN** the API returns all confirmed Courses enrolled by the Learner with their ordered Units, Lessons, goals and current selection

#### Scenario: Foreign course access is neutral
- **WHEN** a Learner attempts to read or select a Course they are not enrolled in
- **THEN** the API responds with a neutral 404 status

#### Scenario: Selection persists
- **WHEN** a Learner selects a Course
- **THEN** that Course becomes active and remains selected across client restarts
