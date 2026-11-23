## ADDED Requirements

### Requirement: Gesture, hotkey and explanation events
The catalogue SHALL include client events `gesture_used` (gesture enum, surface, outcome `committed` or `cancelled`, `undone_within_2s` boolean), `hotkey_used` (key_id enum, surface), `coach_mark_shown` and `coach_mark_dismissed` (coach_id, dismissal `got_it`, `used_feature`, `timeout` or `skipped`), and `toast_shown` (toast_id, kind `info`, `success`, `warning`, `error` or `undo`). Properties MUST be closed enums or stable ids, MUST include the common properties and `source=client`, and MUST NOT contain typed text, key sequences typed into inputs, Slide text or coordinates. Where a gesture or hotkey replaces an existing control the event MUST carry `entry_point` (`gesture`, `hotkey`, `button`) so adoption can be compared with the control it replaces. A hotkey pressed while a text field has focus MUST NOT emit an event. Opt-out MUST stop all of them.

#### Scenario: Swipe closes the answer panel
- **WHEN** a Learner swipes the answer panel closed
- **THEN** `gesture_used` is emitted with gesture `swipe_close`, surface `answer_panel`, outcome `committed` and `entry_point=gesture`, with no free text

#### Scenario: Typing is not a hotkey
- **WHEN** a Learner types "m" in the ask field
- **THEN** no `hotkey_used` event is emitted

#### Scenario: Opt-out
- **WHEN** analytics is disabled
- **THEN** none of the five events is sent

### Requirement: UX simplification metrics
Dashboards SHALL derive these metrics from the events above and existing events, with no new stored data: gesture adoption (Learners using a gesture at least once in 7 days divided by active mobile Learners), hotkey adoption (same for desktop), mis-swipe rate (`gesture_used` with `undone_within_2s=true` divided by committed gestures; target below 5%), cancelled-gesture rate, coach-mark completion (`used_feature` divided by `coach_mark_shown`), and time-to-first-action (seconds from `ui_screen_viewed` to the first `ui_control_activated` or domain event on that screen, median and p90, tracked per screen before and after the copy pass).

#### Scenario: Copy pass effect
- **WHEN** a screen's copy pass ships
- **THEN** its median time-to-first-action is compared with the 14 days before, and a rise above 10% opens a follow-up ticket

### Requirement: UX simplification data boundary
UX simplification SHALL add no table or payload beyond five per-Learner settings (`haptics`, `swipe_navigation`, `single_key_shortcuts`, `gesture_hints_seen`, `hotkey_hints_seen`) stored with the Learner profile. They MUST be included in account export, removed on account erasure and covered by the Postgres backup and restore drill. The change MUST NOT call an AI provider.

#### Scenario: Export and erasure
- **WHEN** a Learner exports or erases their account
- **THEN** the five settings are exported or removed with the profile
