## ADDED Requirements

### Requirement: Content-addressed narration assets
The API SHALL compute a narration content hash from normalized text (Unicode NFC, collapsed whitespace), voice id, model id and the Engine's resolved settings fingerprint before any provider call. Identical hashes MUST share one stored audio and alignment object and MUST trigger at most one synthesis, including under concurrent requests. An existing ready hash MUST be served as `narration_cache_hit` at zero Credits.

#### Scenario: Concurrent identical requests
- **WHEN** two Lessons with identical narration are synthesized at the same time
- **THEN** the provider is called once per distinct hash and both Lessons reference the same objects

#### Scenario: Settings change
- **WHEN** a voice setting changes the fingerprint
- **THEN** a new asset version is created and the old version is retained for recovery

### Requirement: Reference-counted retention
Each asset SHALL carry a reference count of ready slides. An asset and its objects MUST be deleted only after the last reference ends and the configured owner retention period (`NARRATION_RETENTION_DAYS`, default 30) elapses. Backups MUST contain hashes, object keys and reference counts.

#### Scenario: Shared asset survives one deletion
- **WHEN** one of two Lessons sharing an asset is deleted
- **THEN** the objects stay and the count drops to one

#### Scenario: Last reference
- **WHEN** the last reference is deleted
- **THEN** the objects are kept until retention elapses and are then deleted

### Requirement: Narration recovery without charge
The Learner MAY retry failed audio and restore a missing object. Retry MUST re-queue the same content hash. A missing object MUST be restored from the latest retained bucket version, else from a retained prior settings version of the same text, else through a tracked operator regeneration. None of these actions MAY reserve or charge Credits.

#### Scenario: Missing object, no prior version
- **WHEN** an object is missing and nothing can be restored
- **THEN** one regeneration is tracked per hash, the slide shows captions and "being restored", and completion needs no Learner payment
