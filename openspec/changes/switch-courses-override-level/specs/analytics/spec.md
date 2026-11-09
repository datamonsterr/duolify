## ADDED Requirements

### Requirement: Course switcher analytics catalogue
The catalog SHALL include `ui_navigation_selected` (destination) and `learner_profile_updated` (fields_changed). Course switching and level updates SHALL emit these events with platform, app_version and source common properties, respecting analytics opt-out and excluding learner private text.

#### Scenario: Course switched
- **WHEN** a learner selects another course
- **THEN** ui_navigation_selected is emitted with destination

#### Scenario: Level override updated
- **WHEN** a learner overrides or resets a course level
- **THEN** learner_profile_updated is emitted with fields_changed containing level
