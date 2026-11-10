## ADDED Requirements

### Requirement: Authorized object-backed lesson playback
The API MUST return a closed manifest only to an enrolled Learner, containing stable ordered slide IDs, duration_ms, asset URLs, checkpoints and captions, with no assessment answer keys. It MUST authorize every document read, reject malformed manifests and serve fixture objects until generation is available. The host MUST render documents in sandbox="allow-scripts" without allow-same-origin, prepend a strict configured asset-origin CSP, and accept only source/origin/schema-valid active-slide messages. Reaching the last slide MUST offer Take lesson test without granting completion.

#### Scenario: Authorized fixture lesson
- **WHEN** an enrolled Learner opens a fixture lesson
- **THEN** the manifest and versioned HTML load with stable IDs and the last slide links to the test without changing progress

#### Scenario: Unauthorized or unsafe content
- **WHEN** an anonymous or unenrolled caller requests a manifest or document, or a document attempts third-party network/storage access
- **THEN** the API denies the read or the sandbox/CSP blocks access, and malformed messages cannot change host state

#### Scenario: Content unavailable
- **WHEN** an object is missing, corrupt, or the connection is offline
- **THEN** the player shows readable recovery with Retry and Exit, without completion or learner text in analytics

### Requirement: Playback metadata recovery and privacy
Lesson metadata MUST join encrypted nightly 14-day Postgres backups, export and cascade erasure policies. Manifest/HTML/assets MUST use versioned application storage and existing protected asset backup policy. Restores MUST resolve original fixture objects and reapply account deletion tombstones. Device replicas remain disposable. E3-01 MUST NOT introduce AI calls or alter existing reliability gates.

#### Scenario: Restore and erase
- **WHEN** fixture metadata and protected objects are restored and learner deletion is replayed
- **THEN** original content is readable before erasure and inaccessible afterwards without regenerating it

### Requirement: Content-free playback telemetry
The analytics allowlist and catalogue MUST define lesson_started (lesson_id, course_id) and lesson_player_error (lesson_id, slide_id when known, error_code) before emitters. Existing UUID/common properties and opt-out MUST apply; slide or learner text MUST never be captured.

#### Scenario: Private playback event
- **WHEN** playback starts or fails
- **THEN** the PostHog double receives only allowed IDs/fixed codes and common properties, and opt-out suppresses delivery
