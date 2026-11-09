## ADDED Requirements

### Requirement: External payment mapping verification
Before any pricing catalogue version is activated for sale, the API SHALL verify all external payment mappings against provider APIs or test doubles. For Stripe, verification SHALL check that each mapped price exists, is active, matches the configured currency and minor-unit amount, and matches the recurring interval (one-time for packs, monthly count 1 for monthly offers, yearly count 1 for yearly offers) in the target environment. Store-localized product ID mappings SHALL be validated according to channel-specific conventions (Apple, Google). Stripe price IDs from retired versions SHALL be retained immutably in their respective version records.

#### Scenario: Wrong Stripe price amount
- **WHEN** a pricing draft maps a pack or plan to a Stripe price ID with an amount different from the configured catalogue amount
- **THEN** validation or activation is blocked with the mismatching SKU and amounts

#### Scenario: Wrong recurring interval
- **WHEN** a monthly subscription offer is mapped to a yearly or one-time Stripe price
- **THEN** validation or activation is blocked with the interval mismatch

### Requirement: Catalogue economics and margin floor
The API and Engine SHALL verify that every standard action's protected cost per credit does not exceed the configured protected cost ceiling, and that every sellable SKU maintains a paid-AI gross margin of at least 70% under configured fees, refund reserves, and full credit redemption at the ceiling cost. An action exceeding the cost ceiling SHALL block submission unless an audited override reason is explicitly supplied; a SKU falling below 70% margin SHALL always block activation and cannot be overridden. Daily free credits SHALL be sized to match the exact combined envelope of 3 quick notes and 8 short (or 5 long) tutor messages.

#### Scenario: Uneconomic SKU below 70% margin
- **WHEN** a draft sets pack or plan pricing resulting in net margin below 70%
- **THEN** submission and approval are refused regardless of operator role or notice window

#### Scenario: Cost ceiling override
- **WHEN** a standard action exceeds the cost ceiling and an operator provides an explicit override reason
- **THEN** the override is recorded in the audit log and submission is permitted, provided all SKUs meet the 70% margin floor

### Requirement: Verification evidence audit and analytics
Every external mapping check and catalogue economics evaluation SHALL append an immutable audit record to `pricing_audit` with the evaluation evidence, actor hash, and request identifier. Rejections SHALL emit `pricing_version_rejected` to operational telemetry with sanitized properties and without private operator reason text. Verification evidence SHALL be retained with immutable versions for at least seven years.

#### Scenario: Verification failure audited
- **WHEN** validation fails due to invalid provider mapping or uneconomic margin
- **THEN** an audit entry records the failure evidence and `pricing_version_rejected` is emitted with UUID actor and version properties

### Requirement: Configurable payment processing fees and refund reserve
The pricing catalogue configuration schema SHALL permit a `processing_fees` object specifying payment provider fee rates and refund reserves (`pack_fee_rate`, `pack_fixed_fee_usd`, `subscription_fee_rate`, `subscription_fixed_fee_usd`, `refund_reserve_rate`). When omitted in catalogue versions, the engine and API SHALL use the development-proposed default rates (pack: 2.9% + $0.30, subscription: 3.6% + $0.30, refund reserve: 2.0%) flagged as dev-proposed pending final owner review.

#### Scenario: Catalogue with custom processing fees
- **WHEN** a catalogue draft provides `processing_fees`
- **THEN** SKU margin calculations use the configured fee rates and refund reserve rather than hardcoded literals
