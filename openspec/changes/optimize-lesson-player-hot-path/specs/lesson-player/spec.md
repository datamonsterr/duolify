## ADDED Requirements
### Requirement: Lightweight lesson player hot path
The API SHALL authorize every lesson player request against Postgres, never from a cache, and SHALL NOT read object storage for playback position, sync or narration audio requests while the manifest is cached. The decoded manifest SHALL be cached in a bounded in-process LRU keyed by the immutable manifest object key and version, expire after a bounded time, and be read at most once from storage per cold entry. Manifest reads SHALL return the SHA-256 digests stored in the manifest; slide reads SHALL validate the HTML and refuse bytes whose digest differs from the manifest. Malformed lesson or course identifiers SHALL return a client error. The client SHALL save position to the server immediately on pause, seek, checkpoint, slide change, hidden page and exit, and at most every 10 seconds while playing, and SHALL keep writing the local resume position on every save.

#### Scenario: Warm manifest and position save
- **WHEN** an enrolled learner reads a cached lesson manifest or saves position
- **THEN** no object is read from storage and enrollment is still verified in Postgres

#### Scenario: Another learner after a warm cache
- **WHEN** an unenrolled learner requests a lesson another learner has just opened
- **THEN** the API returns 404 for the manifest, slides, position, sync and narration

#### Scenario: Tampered object
- **WHEN** a slide differs from its manifest digest, or a manifest object is replaced and read cold
- **THEN** the API fails closed with a retryable 503 and no private content
