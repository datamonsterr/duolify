## MODIFIED Requirements

### Requirement: Credits
Enrichment MUST reserve and settle Credits per the `credits` spec, drawing from Daily free credits first. When no spendable Credits remain, the note MUST still be saved, enrichment MUST stay pending with status `enrichment_pending`, and the client MUST tell the Learner that enrichment is waiting and show the upsell option without blocking the note. Pending enrichments MUST run automatically, oldest first, when Credits become available, including the next day's Daily free credits.

#### Scenario: Zero credits
- **WHEN** a Learner without spendable Credits saves a note
- **THEN** the note is saved with status `enrichment_pending` and the client shows that enrichment waits, with a top-up option

#### Scenario: Next day
- **WHEN** the next Daily free credits grant arrives
- **THEN** pending enrichments run oldest first until the grant is used

#### Scenario: Allowance covers typical use
- **WHEN** a Learner with only the daily grant saves 3 notes
- **THEN** all 3 are enriched
