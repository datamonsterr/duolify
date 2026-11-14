## ADDED Requirements

### Requirement: Curated voice entitlement is verified server-side
The curated voice benefit MUST be read from verified Postgres subscription terms (active and unrevoked); client claims, balances and cached plan hints MUST NOT grant it. Upgrade guidance for curated voices MUST be withheld from plans whose benefits include "No upsell banners".

#### Scenario: Free Learner
- **WHEN** a Learner without a plan opens the voice panel
- **THEN** curated voices are locked and one upgrade prompt may fire `credits_upsell_shown`

#### Scenario: Learn Learner
- **WHEN** a Learn Learner opens the voice panel
- **THEN** curated voices are locked without an upsell banner
