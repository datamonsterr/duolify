## ADDED Requirements

### Requirement: Owner-approved design chat catalogue
A new immutable Pricing config version SHALL record `design_chat_decision: owner_approved`, `setup_chat=0`, `course_brief_chat=0` and `outline_chat_change=5` Credits, with Daily free credits eligible for the latter. It SHALL retain the interim configured usage/cost envelopes, `outline_replan=55`, and explicit policy that clarifying chats are platform paid, only a valid delivered outline proposal is charged even if discarded, undelivered proposals release reservations, and manual edits, Undo, Apply and Discard are free. Historical versions MUST remain unchanged.

#### Scenario: Production owner approval
- **WHEN** the version passes ordinary notice, cost and mapping validation and the configured `PRICING_OWNER_ID` administrator approves it
- **THEN** it may be scheduled and activated, and new quotes disclose its original prices and eligibility

#### Scenario: Decision or owner missing
- **WHEN** production approval or activation lacks the recorded owner-approved decision or matching owner reviewer
- **THEN** the design chat version is rejected without blocking unrelated catalogue actions

#### Scenario: Development remains available
- **WHEN** a granted administrator approves an interim ADR 0012 version in development or test
- **THEN** the existing interim path remains available

#### Scenario: Forward rollback and retention
- **WHEN** an approved forward version removes only the three design chat actions
- **THEN** unrelated prices including outline_replan remain unchanged, historical quotes retain their version, and encrypted financial backup/restore preserves the version, policy, reviewer audit and activation history for seven-year retention

#### Scenario: Live gates remain independent
- **WHEN** the fixture is committed or drafted
- **THEN** no live activation, sales, feature flags, provider promotion or model/fallback change occurs; existing canonical pricing analytics remain content free
