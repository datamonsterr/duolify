## ADDED Requirements
### Requirement: Atomic disposable opened lesson cache
The client SHALL verify SHA-256 HTML and asset checksums from authorized versioned manifests before atomically publishing a complete account-scoped lesson bundle. It SHALL bound each bundle to 8 MiB and each account to five lessons and 40 MiB, evict only after a successful replacement, and remove private cache and offline identity on logout/account change. Cached documents SHALL retain sandbox="allow-scripts" without allow-same-origin and a network-denying CSP with only verified embedded assets. Server objects SHALL remain authoritative, versioned and covered by existing backup/restore checks; device copies SHALL be disposable and excluded from backups. No new learner tables or AI behavior are introduced. Progress synchronization remains E3-04.

#### Scenario: Restart offline
- **WHEN** the authenticated learner restarts offline after a complete lesson download
- **THEN** the public shell and credential-bound identity bootstrap without network and every slide and asset plays from the verified bundle

#### Scenario: Interrupted replacement
- **WHEN** a download is interrupted, fails its checksum or exceeds quota
- **THEN** no partial lesson is published and the previous complete cached version remains playable

#### Scenario: Evicted or foreign lesson
- **WHEN** a lesson is missing, evicted, corrupt or belongs to another account
- **THEN** no private content appears and accessible Retry and Exit recovery explains offline_uncached

#### Scenario: Preloaded navigation
- **WHEN** a slide plays
- **THEN** the next document and assets are ready before navigation and slow downloads retain readable current content with a loading status

#### Scenario: Recovery analytics
- **WHEN** an opted-in learner selects Retry or Exit for an uncached offline lesson
- **THEN** ui_recovery_selected carries surface=lesson_player, error_code=offline_uncached and action, while lesson_player_error carries a fixed allowlisted code, authenticated UUID and common properties without learner or slide text
