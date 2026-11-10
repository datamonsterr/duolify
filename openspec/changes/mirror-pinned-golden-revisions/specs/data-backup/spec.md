## MODIFIED Requirements

### Requirement: Object storage protection
Object storage buckets for Slides, assets and Golden set mirrored revisions MUST have versioning enabled. Pinned golden set dataset revisions mirrored to S3 MUST be verified with SHA-256 checksums and immutable revision keys. Expected dataset digests MUST be retained in the Git-backed `evals/pinned-datasets.toml` manifest. Restored mirror and cache bytes MUST match that repository pin before use; restored sidecars or object metadata alone MUST NOT establish trust. Local caches remain disposable. Slides MUST be regenerable only through a tracked job, never silently.

#### Scenario: Pinned golden set mirrored
- **WHEN** a pinned dataset revision is mirrored to object storage
- **THEN** it is stored under an immutable revision key with an associated SHA-256 checksum

#### Scenario: Dataset recovered from backup
- **WHEN** a mirrored revision or cache is restored from backup
- **THEN** its exact bytes are verified against the Git-backed dataset/revision digest pin and missing or mismatched checksum metadata fails explicitly
