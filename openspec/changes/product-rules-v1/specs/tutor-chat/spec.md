## MODIFIED Requirements

### Requirement: Credits
Each chat turn MUST reserve and settle Credits as defined in the `credits` spec, drawing from Daily free credits first. The client MUST show the remaining Daily free credits, the balance and the estimated cost class before sending. When the Daily free credits and all other Credits are exhausted, the turn MUST be refused before any Engine call, the typed message MUST be kept as a draft, and the client MUST show the upsell screen with the next Daily free credits time and the top-up options. A refused turn MUST NOT fail silently.

#### Scenario: Insufficient credits
- **WHEN** the spendable balance is lower than the estimate
- **THEN** the turn is refused, no Engine call is made, the draft is kept and the upsell screen is shown

#### Scenario: Daily allowance used first
- **WHEN** a Learner has Daily free credits and purchased Credits
- **THEN** the turn is charged to the Daily free credits first

#### Scenario: Allowance covers typical use
- **WHEN** a Learner with only the daily grant sends 5 long, context-heavy messages
- **THEN** all 5 succeed
