# Apply source-attributed refunds and disputes

## Why
Ticket E7-10 requires implementing compensating financial reversals (partial/full refunds and chargebacks) without editing past ledger history. A refund or dispute must affect only its source payment and attributable future benefits, record non-spendable source debt when forced reversals exceed unspent source credits, block new paid AI while debt is unresolved, preserve prior spends and unrelated paid/daily balances, provide an operator-audited goodwill adjustment path, and display channel-correct recovery in the wallet without private payment details.

## What Changes
- Add support for signed refund and dispute notifications through the test payment provider boundary.
- Transactionally deduplicate partial and full reversals by unique reversal key and cap cumulative reversals at the original refundable payment amount.
- Attribute credit deductions to the source entry; deduct only unspent source credits via compensating ledger entries.
- Record any deficit exceeding unspent source credits as non-spendable source debt in debt history.
- Block new paid AI reservations when active source debt exists, while allowing daily free credit use.
- Revoke affected future subscription grants and entitlements upon full term refund/dispute.
- Document and implement an operator-only audited goodwill adjustment to waive source debt.
- Present channel-correct refund guidance and debt alerts in the wallet/shop matching design system tokens and desktop/mobile states.
- Emit `credits_refunded` and `entitlement_changed` via the transactional financial outbox.
- Retain refund allocations, dispute keys, and debt history for seven years in financial backups with privacy and restore-drill compliance.

## Impact
- API: `services/api/internal/credits` payments, subscriptions, reservations, debt history, migrations, privacy, and backup.
- Client: `apps/client/src/features/credits` wallet, shop, types, and recovery banners.
- Analytics: catalogue and API allowlist update for `credits_refunded`.
- Retention: financial backups, privacy policy, anonymization registry, data inventory.
