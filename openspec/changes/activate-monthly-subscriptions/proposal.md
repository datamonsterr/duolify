## Why

E7-06 verifies one-time payments. E7-07 must activate monthly subscriptions only
from provider-funded invoices, with one allowance per funded service month.

## What Changes

- Extend the signed fixture provider boundary to retrieve canonical subscription
  and invoice evidence, preserving the E7-05 live gate.
- Atomically append funded terms, entitlements, allowance and financial events.
- Show the verified plan and channel management in Wallet and Shop.
- Include immutable history in financial backups, privacy exports and erasure.

## Impact

API credits, client credits/pricing, analytics catalogue, financial backup fixtures.
No AI behavior changes, credentials, new environment variables or live adapter.
