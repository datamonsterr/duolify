## ADDED Requirements

### Requirement: Course list summaries
`GET /v1/courses` SHALL return the authenticated Learner's confirmed enrolled Courses as summaries including card progress, without a unit or Lesson tree. The detail route SHALL provide the tree. The list SHALL be private and revalidated with an ETag.

#### Scenario: List courses
- **WHEN** an authenticated Learner requests the Course list
- **THEN** the response contains Course summaries and the active Course id without full unit or Lesson trees
