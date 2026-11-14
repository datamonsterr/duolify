## ADDED Requirements

### Requirement: Enrollment analytics events
The API SHALL emit `course_enrolled_published` (course_id: UUID of the copy, source_course_id: UUID, source: `link`, version: int) once when a copy is created, and `published_lesson_reused` (course_id: UUID, lesson_id: UUID, version: int) when a reused Lesson becomes available. Events MUST NOT contain tokens, usernames, titles, Slide content or answers, and MUST respect analytics opt-out.

#### Scenario: Enroll event once
- **WHEN** a Learner enrolls twice through a link
- **THEN** `course_enrolled_published` is emitted once with source `link`
