## ADDED Requirements

### Requirement: Course switcher and per-course level controls
The client SHALL render the enrolled Course catalog matching canvases S03, S08, and S23 with current-course markers, progress indicators, confirmed edited titles, and ordered lesson goals. The Settings/Profile view SHALL provide per-course level controls to view, override, and reset each enrolled course's level. Course switching SHALL emit `ui_navigation_selected` and level updates SHALL emit `learner_profile_updated`, respecting analytics opt-out.

#### Scenario: Switching courses
- **WHEN** a learner selects another enrolled course
- **THEN** the selection persists, the path view updates to the selected course's outline, and `ui_navigation_selected` is emitted if opted in

#### Scenario: Override course level from settings
- **WHEN** a learner overrides or resets a course level
- **THEN** the course's level indicator updates without modifying the global profile
