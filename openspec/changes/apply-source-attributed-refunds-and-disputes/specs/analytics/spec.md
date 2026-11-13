## ADDED Requirements

### Requirement: Refund analytics catalogue
The analytics catalogue SHALL define `credits_refunded` with properties `amount`, `payment_reference`, `reason_code`, `source`, `platform`, and `app_version`. The event MUST be emitted through the transactional financial outbox and deduplicated by reversal operation key. It MUST honor analytics opt-out and MUST NOT contain card numbers, bank details or learner text.

#### Scenario: Verified refund outbox event
- **WHEN** a provider refund notification commits to the database
- **THEN** exactly one `credits_refunded` event is enqueued in the financial outbox with credit amount, payment reference and reason code
