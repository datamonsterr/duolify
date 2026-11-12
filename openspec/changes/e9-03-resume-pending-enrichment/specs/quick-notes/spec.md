## MODIFIED Requirements

### Requirement: Credits
Enrichment MUST reserve and settle Credits per the `credits` spec, drawing from Daily free credits first. When no spendable Credits remain, the note MUST still be saved, enrichment MUST stay pending with status `enrichment_pending`, and the client MUST tell the Learner that enrichment is waiting and show the upsell option without blocking the note. Pending enrichments MUST run automatically, oldest first, when Credits become available, including the next day's Daily free credits or purchase top-ups. Deduplication MUST prevent double enrichment, and stale raw revisions MUST NOT be overwritten by old enrichments.

#### Scenario: Zero credits
- **WHEN** a Learner without spendable Credits saves a note
- **THEN** the note is saved with status `enrichment_pending` and the client shows that enrichment waits, with a top-up option

#### Scenario: Next day
- **WHEN** the next Daily free credits grant arrives or credits are purchased
- **THEN** pending enrichments run oldest first until the eligible credit budget is exhausted

#### Scenario: Stale raw revision
- **WHEN** a note is edited while pending enrichment
- **THEN** previous enrichment for the stale revision does not overwrite the updated note
