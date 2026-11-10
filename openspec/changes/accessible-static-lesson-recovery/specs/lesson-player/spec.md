## ADDED Requirements

### Requirement: Accessible static lesson recovery
The player SHALL provide accessible non-animated static playback when reduced motion is preferred or active, presenting a meaningful static visual frame and a full instructional explanation. When a slide or required asset is missing or fails to load, the player SHALL present a recovery state with Retry and Exit actions, preserve the learner's position cursor, keep captions and transcript accessible, and disable advancement so that incomplete or failed lessons never grant completion or test progression.

#### Scenario: Reduced motion playback
- **WHEN** reduced motion is active
- **THEN** animations are suppressed, the static visual frame and full explanation are displayed, and playback controls and transcript operate accessibly with keyboard and screen reader

#### Scenario: Missing or failed asset recovery
- **WHEN** a slide asset is missing or fails to load
- **THEN** the player displays "This slide didn't load", offers Retry slide and Exit actions, retains the learner's place, leaves the transcript accessible, and disables Next without granting completion

### Requirement: Manifest explanation contract
The lesson manifest SHALL include an instructional `explanation` field for each slide in both the storage schema and public projection DTO. The API SHALL validate that explanation length is bounded (up to 20,000 characters) and does not disclose private assessment answers or storage keys.

#### Scenario: Manifest explanation delivered
- **WHEN** an authorized learner requests a lesson manifest
- **THEN** each slide in the manifest includes an instructional explanation and no private assessment data is leaked

### Requirement: Static recovery analytics catalogue
The analytics catalogue and API allowlist SHALL register `lesson_player_error` with fixed error code `asset_unavailable` and `ui_recovery_selected` with `surface=lesson_player`, `error_code=asset_unavailable`, and `action=retry|exit`. Telemetry MUST include UUID identity and common properties, respect opt-out, and never include slide, narration or learner text.

#### Scenario: Asset failure and recovery telemetry
- **WHEN** an asset fails and the learner selects Retry or Exit
- **THEN** `lesson_player_error` and `ui_recovery_selected` are emitted with allowlisted codes and UUID properties, and opt-out suppresses all delivery
