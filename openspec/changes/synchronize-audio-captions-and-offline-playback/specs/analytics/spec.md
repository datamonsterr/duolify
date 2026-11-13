## ADDED Requirements

### Requirement: Narration control and caption telemetry
The catalogue SHALL define client events `narration_muted` (carrying `lesson_id`, `slide_id`, and boolean `muted`), `narration_speed_changed` (carrying `lesson_id`, `slide_id`, and numeric `speed`), and `captions_toggled` (carrying `lesson_id`, `slide_id`, and boolean `captions`). All events MUST include Learner distinct id, platform, source, app_version, and common properties, MUST honor user opt-out, and MUST NOT contain narration text, captions, audio URLs, or slide content.

#### Scenario: Mute narration
- **WHEN** the Learner toggles the mute control
- **THEN** PostHog receives `narration_muted` with `muted: true` or `false` and no narration text

#### Scenario: Change playback speed
- **WHEN** the Learner selects a playback speed (0.75, 1, 1.25, 1.5)
- **THEN** PostHog receives `narration_speed_changed` with the new speed value

#### Scenario: Toggle captions
- **WHEN** the Learner toggles captions visibility
- **THEN** PostHog receives `captions_toggled` with the current caption visibility state
