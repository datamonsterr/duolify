# Verify external mappings and catalogue economics design

## Context
Ticket E12-07 ensures that external provider payment mappings and AI catalogue economics are verified prior to activating any pricing catalogue version for sale. The system compares provider rates against the approved table and verified usage envelopes rather than blindly accepting drift or unverified IDs.

## Architecture

### 1. External Mapping Verification
- `StripeVerifier` interface abstracts Stripe API checks:
  - Price ID exists and is active.
  - Amount matches `amount_minor` (or `price_minor`).
  - Currency matches catalogue currency (case-insensitively).
  - Recurring interval matches plan interval (one-time for packs, monthly with count 1 for monthly plans, yearly with count 1 for yearly plans).
  - Environment matches (test vs live).
  - Old price IDs of retired versions are retained and cannot be overwritten.
- Store mapping validation:
  - Channel-specific checks for `apple` and `google` store product IDs.
  - Formats: non-empty, valid channel character sets, unique per SKU within channel.

### 2. Catalogue Economics & Margin Floor
- **Protected Cost per Credit**:
  - For standard actions (`preview`, `outline`, `lesson_content`, `narration`, `tutor_short`, `tutor_long`, `quick_note`), calculate protected USD cost using configured model rates and bounds.
  - Cost per credit = `protected_cost / action_credits`.
  - Must not exceed `limits.protected_cost_ceiling_usd_per_credit` (0.0015 USD).
  - If exceeded, submission is blocked unless an explicit override reason is provided (E12-04 override rule), which is recorded in the audit log.
- **70% Margin Floor**:
  - For all sellable SKUs (packs, monthly plans, yearly plans):
    - `net_receipts = sale_price - fees - refunds`.
    - `margin = (net_receipts - protected_ai_cost) / net_receipts * 100`.
    - Under configured fees (e.g., Stripe 2.9% + $0.30 or subscription 3.6% + $0.30, or store commission 15-30%) and refund reserves (2%), and assuming full redemption of included credits at the ceiling cost ($0.0015/credit).
    - If margin < 70%, activation/approval is ALWAYS blocked. Under ADR 0011, the 70% margin floor cannot be overridden.
- **Exact Daily Allowance Sizing**:
  - `daily_free.credits` must match `3 * quick_note + 8 * tutor_short` (and `3 * quick_note + 5 * tutor_long`).

### 3. Verification Evidence & Analytics
- Verification evidence is recorded in `pricing_audit` and retained for at least 7 years.
- Failures emit `pricing_version_rejected` with sanitized details (UUID properties, no private text or secrets).
