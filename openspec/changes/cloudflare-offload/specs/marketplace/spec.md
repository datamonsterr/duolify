## MODIFIED Requirements

### Requirement: Derived cache
Redis SHALL only cache result pages derived from Postgres. Losing Redis MUST rebuild on the next read, and a delist, relist or new listing MUST remove every affected origin cache page immediately. The Edge Worker MAY cache eligible anonymous browse and Course page projections internally, including on the Go fallback. It MUST check `MARKETPLACE_ENABLED` and rate limits before serving cache entries, exclude authenticated/viewer-specific data, keep the existing public-facing `no-store` and `noindex` policy, and cap cumulative stale age at `MARKETPLACE_CACHE_TTL_SECONDS` across all layers. An outage MUST NOT extend that cap for hidden or delisted content. The Go path remains available for viewer block rules and fallback.

#### Scenario: Delist leaves the cache
- **WHEN** a cached page contains a Course and the owner delists it
- **THEN** the Go cache is invalidated immediately and the Edge Worker stops serving the delisted Course within the configured TTL

#### Scenario: Moderation hide reaches all edge reads
- **WHEN** moderation hides a listed Course with warm browse and Course page entries
- **THEN** both surfaces exclude it no later than the configured TTL, including on a Go-cache fallback

#### Scenario: Flag turned off with warmed entries
- **WHEN** the Marketplace flag is off
- **THEN** the next browse or Course page read is a neutral 404 regardless of cached results
