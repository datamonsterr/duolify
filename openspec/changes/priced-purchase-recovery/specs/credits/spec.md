## ADDED Requirements

### Requirement: Safe priced recovery
The client SHALL show catalogue-owned packs, benefits, full billed yearly totals, monthly grants, rollover, taxes, renewal terms, daily eligibility and the next server-confirmed local reset. No recurring offer SHALL be preselected. Insufficient actions SHALL retain mounted input and raw notes. Resume SHALL restore review without sending or charging.

#### Scenario: Insufficient action return
- **WHEN** an action lacks eligible Credits and the learner returns from offers
- **THEN** its saved work and review remain available without automatic reservation or generation

### Requirement: Illustrative checkout isolation
The API SHALL bind pending illustrative orders to the authenticated learner and immutable configured terms, validate quote ownership and fixed internal return targets, and reject stale pricing or unsupported native channels. Illustrative orders SHALL never grant Credits or become verified. Real billing remains disabled until separately integrated and verified.

#### Scenario: Forged successful callback
- **WHEN** an illustrative checkout returns with success parameters or an unrelated wallet increase
- **THEN** status remains pending and no Credits are granted

### Requirement: Recovery financial backup and privacy
Pending orders SHALL enter encrypted financial backups with seven-year live retention and 35-day snapshot rotation. Account export SHALL include owned orders; deletion and restore tombstones SHALL sever personal identity while preserving pseudonymous financial history. Local recovery SHALL contain only disposable identifiers.

#### Scenario: Restore pending order
- **WHEN** financial state is restored
- **THEN** original order terms and pending status survive and erased learners remain unlinked

### Requirement: Recovery analytics privacy
The client SHALL emit paywall_viewed, credits_upsell_shown, pricing_version_exposed, ui_purchase_option_selected and ui_checkout_requested through the existing UUID/consent wrapper. Only fixed identifiers, configured amounts and balances SHALL be included; no learner content SHALL be captured. AI behavior and reliability gates SHALL remain unchanged.

#### Scenario: Opted out learner
- **WHEN** an opted out learner reviews offers and requests illustrative checkout
- **THEN** the flow works without any PostHog delivery
