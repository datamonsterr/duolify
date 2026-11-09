## MODIFIED Requirements

### Requirement: Outline-only generation
After explicit final cost consent, the system SHALL create an outline-only River job from the latest learner-confirmed edited outline revision. This replaces older read-only/sample-outline wording: ordered units, lesson titles/goals, additions and removals MUST be preserved exactly; unapplied proposals MUST NOT enter the job. The private GenerateCourseOutline request MUST carry the edited outline, draft revision and trusted quote bounds. Output containing Slides or Lesson tests or differing from the confirmed structure MUST be rejected within the bounded evaluated fallback policy.

#### Scenario: Confirmed edited outline creates a course
- **WHEN** the learner accepts the revision-bound quote and confirms Create
- **THEN** validated success atomically stores the Course, ordered Units, locked Lesson stubs and one enrollment
- **AND** only the first Lesson is ready_to_generate and no lesson content is generated

#### Scenario: Stale or malformed output
- **WHEN** the revision changed before submission or output changes authored structure or includes lesson content
- **THEN** stale submission is rejected before provider work or invalid output fails bounded validation
- **AND** terminal failure releases undelivered reserved Credits and leaves no partial enrollment

### Requirement: Generation is a background job
Outline creation SHALL run as a durable River job, never on the request path. Duplicate submissions for a confirmed draft MUST attach to one creation job, reservation and enrollment. Durable stages SHALL stream through authenticated SSE and support reconnect without repeating provider work or settlement.

#### Scenario: Worker restarts
- **WHEN** a worker restarts after validation or settlement
- **THEN** it resumes the same stored output and finalization with exactly one enrollment and settlement
- **AND** an interrupted provider call without durable output fails closed with released undelivered Credits

## ADDED Requirements

### Requirement: Confirmed creation data and reliability
Creation snapshots, Courses, Units, Lesson stubs and enrollment SHALL be covered by nightly backup fixtures, learner export and account erasure policies in the inventory. Course deletion SHALL retain the existing 30-day soft-delete grace. Analytics SHALL emit course_outline_generated, course_enrolled, course_generation_failed and $ai_generation through allowlisted wrappers without learner text. Mock-provider course_outline Golden cases SHALL check structure, empty titles, no Slides/tests, authored revisions and profile handling and pass engine:eval; live promotion remains E10-06.

#### Scenario: Restore and privacy
- **WHEN** a backed-up enrolled outline is restored
- **THEN** its order, goals, first-ready status and single enrollment survive
- **AND** account export includes its data and account deletion erases learner content via cascades
