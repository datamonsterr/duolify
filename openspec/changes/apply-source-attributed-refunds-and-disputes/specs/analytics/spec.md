## ADDED Requirements

### Requirement: Refund analytics catalogue
The analytics catalogue SHALL define `credits_refunded` with properties `amount`, `payment_reference`, `reason_code`, `source`, `platform`, and `app_version`. The event MUST be emitted through the transactional financial outbox and deduplicated by reversal operation key. It MUST honor analytics opt-out and MUST NOT contain card numbers, bank details or learner text.

#### Scenario: Verified refund outbox event
- **WHEN** a provider refund notification commits to the database
- **THEN** exactly one `credits_refunded` event is enqueued in the financial outbox with credit amount, payment reference and reason code

### Requirement: Entitlement change on reversal
`entitlement_changed` SHALL be emitted exactly once when a refund or chargeback revokes a subscription term (reason_code `refunded` or `disputed`) and once per operator goodwill waiver with only a `reason_code`; operator identity and free-text notes MUST NOT enter analytics.

#### Scenario: Goodwill waiver analytics
- **WHEN** an operator waives source debt with private notes
- **THEN** the outbox event carries the reason code only and the notes remain in the database audit table
