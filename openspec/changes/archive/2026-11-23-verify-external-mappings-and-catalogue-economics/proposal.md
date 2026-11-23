# Verify external mappings and catalogue economics

Validate external payment mappings (Stripe price IDs, recurring intervals, active amounts, environments, channel-specific store IDs) and measured AI envelopes before any catalogue is activated for sale. Compare provider costs against the approved table rather than silently replacing it.

## Key Changes
- Enforce Stripe price verification: exists, active, amount, currency, recurring interval, environment, and retention of retired price IDs. Channel-specific store ID validation (Apple, Google).
- Enforce catalogue economics: reject any standard action whose protected cost per credit exceeds the configured ceiling unless an explicit audited override reason is recorded (E12-04 override rule). Reject any SKU whose paid-AI margin is below 70% under configured fees, refund reserves, and full redemption (ADR 0011: 70% margin floor always blocks activation). Enforce exact daily allowance sizing.
- Verify primary and fallback provider/model IDs and rates from official catalogues with dated evidence.
- Retain mapping and economic verification evidence with immutable versions and audit for seven years in Postgres and nightly financial backups.
- Telemetry: emit `pricing_version_rejected` and sanitized validation outcomes with common UUID properties, suppressing reason text and private tokens.
- AI reliability: add Golden set envelope cases for standard usage, cache, cold-context, narration, and cost-change envelopes under offline evaluation.
