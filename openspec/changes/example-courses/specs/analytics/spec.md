## ADDED Requirements

### Requirement: Example course events
The catalogue SHALL include `example_courses_viewed` (surface: courses|learn_empty|onboarding) and `example_course_enrolled` (slug from the closed list of authored example slugs, surface: courses|learn_empty|onboarding, and the copy's course_id). Events SHALL use UUID/common properties, honor opt-out and consent, and carry no free text, titles or Lesson content. Lesson events keep their existing properties; no content-source field is added because the enrollment event carries the copy's course_id for joining.

#### Scenario: Learner starts an example
- **WHEN** a Learner opens the examples section and enrolls
- **THEN** one viewed event and one enrolled event with closed-enum properties are emitted, and none for an opted-out Learner
