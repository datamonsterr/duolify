## MODIFIED Requirements

### Requirement: Object storage protection
Object storage buckets for Slides, assets and Golden set mirrored revisions MUST have versioning enabled. Pinned golden set dataset revisions mirrored to S3 MUST be verified with SHA-256 checksums and immutable revision keys. Slides MUST be regenerable only through a tracked job, never silently.

#### Scenario: Pinned golden set mirrored
- **WHEN** a pinned dataset revision is mirrored to object storage
- **THEN** it is stored under an immutable revision key with an associated SHA-256 checksum
