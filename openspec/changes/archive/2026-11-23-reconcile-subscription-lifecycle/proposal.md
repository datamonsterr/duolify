# Reconcile subscription lifecycle

## Why
E7-09 needs canonical renewal, dunning, reversible cancellation and future plan changes without extending unpaid benefits.

## What Changes
- Append canonical lifecycle and versioned next-renewal transition history to funded subscriptions.
- Reuse signed Stripe fixtures; add a provider-neutral transition boundary with Stripe scheduling only.
- Resolve generation priority and concurrency from funded configured benefits on every claim; retain all provider/USD caps.
- Show payment failures and effective dates, allow plan management and suppress subscriber upsells.

## Impact
API credits, wallet/shop, analytics catalogue, financial backup/export/deletion. No new AI behavior or live provider credentials.
