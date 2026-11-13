## ADDED Requirements

### Requirement: Marketplace listing metadata is backed up
`marketplace_listings` and `marketplace_listing_stats` SHALL be included in the nightly encrypted Postgres dump. Restore MUST return listings with their search vector inputs, and account deletion MUST erase the owner's listings. Redis holds only derived pages and is not backed up.

#### Scenario: Restore rebuilds browsing
- **WHEN** a dump with a listing is restored and Redis is empty
- **THEN** the catalogue lists the Course and an erased owner's listing is gone
