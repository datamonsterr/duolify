## ADDED Requirements

### Requirement: Signed test-mode pack fulfillment
The API SHALL create test-mode provider Sessions only through an explicitly injected provider, using configured pack terms and synthetic price mappings. It SHALL reject live mode until E7-05 enables a separately reviewed integration. Raw-body v1 HMAC-SHA256 and a five-minute timestamp window SHALL precede durable receipt. Canonical paid Session state and matching order, learner pseudonym, environment, SKU, price, amount and currency SHALL precede an atomic purchase, event outcome and financial outbox. Event ID and environment-scoped PaymentIntent SHALL both be unique. Inbox work SHALL survive provider failure and process restart. Late unpaid events SHALL NOT undo a purchase.

#### Scenario: Delayed and reordered payment
- **WHEN** unpaid completion, asynchronous payment, repeated and reordered notifications arrive
- **THEN** pending state grants nothing and canonical paid state appends exactly one purchase and one credits_purchased outbox event

#### Scenario: Invalid evidence
- **WHEN** a signature, timestamp, canonical binding or mode fails validation
- **THEN** no purchase is appended and browser success flags cannot change server state

### Requirement: Payment lifecycle privacy and recovery
Payment bindings, inbox references and outcomes SHALL remain append-only for seven years, with encrypted 35-day financial snapshots. Separately stored allowlisted redacted envelopes SHALL expire after 30 days. Exports SHALL include linked payment records; deletion and restored tombstones SHALL sever learner identity through the original order while retaining dedupe keys. UI SHALL show canonical pending, failed, expired and verified states with explicit refresh and return-to-work actions at desktop/mobile widths, with keyboard, error, dark and reduced-motion parity. Financial analytics checkout_started, credits_purchased and credits_purchase_failed SHALL use the canonical catalogue, unique outbox, common properties and opt-out without learner text. No AI behavior SHALL change.

#### Scenario: Restored payment replay
- **WHEN** financial snapshots are restored and signed events replayed after account erasure
- **THEN** references and balances remain intact without a duplicate grant, personal identity or analytics delivery
