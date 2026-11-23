## MODIFIED Requirements

### Requirement: Pricing analytics events
The system SHALL emit `pricing_version_exposed` (pricing_version, surface: `paywall`, `quote`, `pack_list`, `plan_list`) at most once per session, surface and version, and `pricing_version_created`, `pricing_version_approved`, `pricing_version_rejected`, `pricing_version_activated` and `pricing_version_rolled_back` (pricing_version, change_class: `adverse` or `non_adverse`) from the API. Events MUST NOT contain price secrets, Stripe keys or reason text. Existing monetization events use the same version id as `price_table_version`.

#### Scenario: Paywall shown
- **WHEN** a client shows the paywall twice in one session
- **THEN** one `pricing_version_exposed` with surface `paywall` is emitted

#### Scenario: Activation
- **WHEN** a scheduled version becomes active
- **THEN** `pricing_version_activated` is emitted once
