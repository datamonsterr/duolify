## ADDED Requirements

### Requirement: Profile completion event
The API SHALL emit `learner_profile_updated` with changed field names only and `onboarding_completed` without domain properties after committed profile confirmation. Both events SHALL use the learner UUID, API source and common platform/app version properties, and SHALL respect analytics opt-out.

#### Scenario: First explicit completion
- **WHEN** a learner first saves or skips setup
- **THEN** `onboarding_completed` is delivered once, and subsequent saves do not emit it again

#### Scenario: Profile field update
- **WHEN** a learner changes one or more profile values
- **THEN** `learner_profile_updated` includes only the names of effectively changed fields
