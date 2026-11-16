## ADDED Requirements

### Requirement: Approved placeholder brand identity components and assets
The client application SHALL provide dedicated accessible `BrandMark`, `BrandWordmark`, and composite `Brand` components utilizing the approved two-tile vector geometry and lowercase wordmark. The mark MUST remain visually legible at 16px and icon sizes. All vector assets SHALL use approved original geometry without third-party game likeness. The client MUST provide Git-backed app icons for Tauri and a web manifest and favicon in the web build.

#### Scenario: Mark rendering at small icon sizes
- **WHEN** the brand mark is displayed at 16px or navigation size
- **THEN** both geometric tiles maintain clear separation and token contrast

#### Scenario: Accessible brand header
- **WHEN** an assistive technology inspects the brand header
- **THEN** an accessible name "Duolify" is exposed and decorative tiles are hidden from screen readers

### Requirement: Optional feedback cues and settings
The client SHALL provide independent settings for UI sounds and haptics on the Settings screen (S23). UI sound cues SHALL remain silent no-ops in v1 without downloading audio assets over the network. Haptic feedback SHALL trigger only on user-initiated actions when supported by the platform, MUST NOT punish incorrect answers, and MUST be suppressed when reduced motion is requested by the learner or the operating system.

#### Scenario: UI sound cue trigger with sound disabled or missing assets
- **WHEN** a user activates a UI control with sounds enabled
- **THEN** no network audio request is dispatched and playback completes silently

#### Scenario: Reduced motion suppresses celebratory haptics
- **WHEN** the learner enables reduced motion or the operating system prefers reduced motion
- **THEN** tactile celebratory vibrations are suppressed

### Requirement: Feedback and identity analytics events
The analytics catalogue SHALL include `ui_control_activated` with surface `settings` and control IDs `ui_sounds` and `haptics`. Telemetry MUST NOT emit repeated cues for purchase success or learner private text.

#### Scenario: Feedback setting toggle
- **WHEN** an opted-in learner toggles UI sounds or haptics
- **THEN** one `ui_control_activated` event is emitted with the corresponding control ID

### Requirement: Brand assets and feedback data backup
Brand assets and web manifest SHALL be Git-backed source files. Feedback preferences SHALL be persisted in `learner_profiles` within PostgreSQL, included in nightly encrypted dumps, and permanently deleted upon account erasure.

#### Scenario: Settings restored from dump
- **WHEN** a learner profile is restored from backup
- **THEN** saved UI sounds and haptics preferences are restored to their saved values

### Requirement: AI reliability boundary
The brand identity and feedback controls SHALL NOT invoke AI models or modify existing engine evaluation golden sets or fallback gates.

#### Scenario: Offline feedback
- **WHEN** the client is offline
- **THEN** brand assets render locally and feedback settings operate without AI provider contact
