## ADDED Requirements

### Requirement: Authored example objects are protected
The versioned object protection and the restore drill SHALL include the immutable objects of every authored example Course version (slides and manifests under `lessons/examples/`), byte for byte, and the nightly asset listing SHALL include example manifest keys. `course_copies.example_slug` SHALL be classified in the backup anonymizer registry as a non-personal closed value. Account export SHALL list the learner's example enrollment and erasure SHALL remove it with the copy.

#### Scenario: Restore authored example objects
- **WHEN** the example objects are lost and restored from the protected copy
- **THEN** the restored bytes equal the Git-authored bundle and every enrolled learner's manifest digests still verify
