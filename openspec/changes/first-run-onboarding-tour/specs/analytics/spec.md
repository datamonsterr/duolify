## ADDED Requirements

### Requirement: Welcome and tour funnel catalogue
The client SHALL emit onboarding_welcome_shown, onboarding_path_chosen with path topic/setup_chat/example/skip, onboarding_tour_started with source first_run/settings, onboarding_tour_step_viewed with step path/next_lesson/credits/progress/notes/courses/settings, onboarding_tour_completed and onboarding_tour_skipped with at_step from the same step enum. The existing consent-aware wrapper SHALL enforce learner UUID and common platform/app_version/source properties, and reject unknown enums. No free text SHALL be captured. Existing sign-up and lesson events SHALL remain joinable by learner UUID.

#### Scenario: Consent disabled
- **WHEN** an opted-out learner uses welcome or tour
- **THEN** no onboarding events are captured
