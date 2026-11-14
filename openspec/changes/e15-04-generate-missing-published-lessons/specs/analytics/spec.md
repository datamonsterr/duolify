## ADDED Requirements

### Requirement: On-demand generation analytics event
The API SHALL emit `published_lesson_generated_on_demand` (course_id: UUID of the copy, lesson_id: UUID, version: int) once after a committed settlement of a Lesson generated in a Course copy. `lesson_generated` and `$ai_generation` are emitted by the reused pipeline. Events MUST contain UUIDs, enums and amounts only, never titles, Slide content or answers, and MUST respect analytics opt-out.

#### Scenario: One event per generated Lesson
- **WHEN** a missing Lesson is generated and settled
- **THEN** `published_lesson_generated_on_demand` is delivered once with UUID properties and nothing when the Learner opted out
