## ADDED Requirements

### Requirement: Course recovery analytics
The analytics catalogue SHALL include `ui_recovery_selected` with properties `surface` (`course_create`, `courses`), `error_code`, `action`, `course_id`, and `screen_id`. When course outline creation fails or is retried, or course removal is handled, the corresponding recovery action SHALL be emitted through PostHog wrappers respecting learner opt-out, UUID identity, and excluding learner text. The API allowlist SHALL retain `ui_recovery_selected` and `course_generation_failed`.

#### Scenario: Creation failure retry selected
- **WHEN** a learner selects retry after course creation failure
- **THEN** `ui_recovery_selected` is emitted with `surface=course_create`, `action=retry`, and no private prompt or outline text
