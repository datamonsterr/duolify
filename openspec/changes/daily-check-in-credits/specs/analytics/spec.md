## ADDED Requirements

### Requirement: Check-in grant analytics
The API MUST enqueue `check_in_credits_granted` atomically with each positive attendance grant, idempotently, with only closed non-PII `amount`, `first_time`, `banked_after` and `capped` properties plus canonical common properties. Delivery MUST respect existing analytics opt-out and MUST NOT affect grant correctness. No toast event is required. Event properties MUST follow existing outbox retention and backup rules.

#### Scenario: Consent and replay
- **WHEN** an opted-out Learner checks in or a committed request is retried
- **THEN** Credits are correct and no opted-out or duplicate event is delivered
