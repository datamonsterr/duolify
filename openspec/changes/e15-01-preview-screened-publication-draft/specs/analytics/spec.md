## ADDED Requirements

### Requirement: Screening event catalogue
The analytics catalogue SHALL include `course_screening_completed` (`verdict` pass|needs_review|block|pending_screening, `lessons_sampled`, `finding_count`, `fallback_used`) and `$ai_generation` with `feature=course_screen`. Events MUST use the Learner UUID, common properties and respect opt-out. Course titles, Slide text, findings text and verdict reasons beyond the closed codes MUST NOT be sent. `course_published` MUST NOT be emitted until visibility is live.

#### Scenario: Screened draft
- **WHEN** a draft finishes screening
- **THEN** one `course_screening_completed` and one `$ai_generation` per model attempt are delivered with no Course text
