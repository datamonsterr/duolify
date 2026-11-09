## ADDED Requirements

### Requirement: Entitled offline course library download
The API SHALL provide a course library download authorization endpoint for enrolled learners. It SHALL verify server-side that the learner holds an active subscription whose verified plan benefits include "Offline course library downloads", reading plan benefits from the authoritative active pricing configuration rather than client-supplied or hardcoded values. When authorized, the endpoint SHALL return the course's generated and unlocked lessons available for download. When the learner lacks an active entitled subscription or the subscription has expired, the endpoint SHALL refuse bulk download authorization with status 403 and error code subscription_required. Universal caching and offline replay of individually opened lessons SHALL remain available across all accounts regardless of subscription state.

#### Scenario: Active subscriber downloads course library
- **WHEN** an authenticated learner with an active Learn, Plus, or Pro subscription requests bulk download for an enrolled course
- **THEN** the API authorizes the download and returns downloadable unlocked lesson descriptors

#### Scenario: Expired or unentitled learner requests library download
- **WHEN** an authenticated learner with an expired subscription or free tier requests course library bulk download
- **THEN** the API rejects the request with status 403 and error code subscription_required without altering existing cached lessons

### Requirement: Resumable library download and storage controls
The client SHALL allow entitled learners to initiate resumable bulk downloads of unlocked lessons for an enrolled course, showing progress and storage controls. Downloads MUST be verified atomically using SHA-256 manifest and asset checksums before publication into the per-account cache. The client SHALL provide storage controls showing downloaded lesson count and approximate usage, and allow clearing downloaded courses. The download index SHALL be device-disposable and MUST be cleared on logout or session expiration.

#### Scenario: Resumed bulk download
- **WHEN** an interrupted course library download is resumed
- **THEN** already downloaded and verified lessons are preserved, and download continues with remaining unlocked lessons

#### Scenario: Expired subscription offline replay
- **WHEN** a subscription expires after an offline course library has been downloaded
- **THEN** already downloaded lessons remain playable offline, but new bulk downloads and future lesson generation cannot be initiated

### Requirement: Library download interaction analytics catalogue
The analytics catalogue and API allowlist SHALL register `ui_control_activated` with `control_id=download_library`, `surface=courses` or `course_path`, and `course_id`, and `ui_recovery_selected` with `action=retry|exit` and error codes including `subscription_required` and `download_failed`. Telemetry MUST include authenticated UUID identity and common properties, honor opt-out, and never contain learner text, slide HTML, or URLs.

#### Scenario: Download library control activated
- **WHEN** an opted-in learner activates course library download
- **THEN** `ui_control_activated` is emitted with `control_id=download_library` and common properties without learner text
