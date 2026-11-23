## ADDED Requirements

### Requirement: Public read Worker conformance and fallback
The Edge Worker SHALL be eligible to serve anonymous share previews, public Course pages, the shared demo and flag-gated Marketplace browse. Its primary data access MUST use Hyperdrive over Cloudflare Tunnel with a least-privilege read-only Postgres role limited to the authorized projections. Conformance MUST compare Edge and Go status, visibility, response fields, ordering, pagination, withdrawal, moderation and auth behavior on the same seeded data. Failed conformance MUST select edge-caching of the applicable anonymous Go read responses. Go MUST serve authenticated or viewer-specific responses and authoritative writes. Edge cache entries MUST be bounded and reconstructable from the backed-up origin records or authored fixtures.

#### Scenario: Go and Hyperdrive conformance passes
- **WHEN** the suite compares the Worker and Go on the same eligible, removed, private and invalid cases
- **THEN** both match the contract, the database role cannot write or read private tables and Hyperdrive is eligible for the release gate

#### Scenario: Go and Hyperdrive conformance fails
- **WHEN** the Worker projection or role cannot satisfy a required conformance case
- **THEN** the Worker uses eligible Go reads as the cache origin and preserves the origin access and freshness rules

### Requirement: Turnstile on share previews
The Go API MUST verify fresh Turnstile proof before a protected share preview is returned on any data path. Cached responses MUST preserve `noindex`, neutral errors, `no-store` for link-gated bodies and immediate revocation or rotation checks. The Worker MUST apply proof, token-authority and rate-limit gates before returning an internally cached anonymous projection. Shared caches MUST exclude viewer-specific or authenticated response data, raw share credentials and proof tokens. Existing enrollment remains protected by Go.

#### Scenario: Warm preview and invalid proof
- **WHEN** a visitor requests a cached share preview with invalid or missing Turnstile proof
- **THEN** Go verification prevents disclosure and the client can try again with fresh proof

#### Scenario: Revoked preview
- **WHEN** the owner revokes or rotates a link with a warmed cache entry
- **THEN** the next open using the old link returns the same neutral not-found as Go
