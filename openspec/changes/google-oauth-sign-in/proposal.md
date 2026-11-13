# Google OAuth sign-in

Add Google sign-in to the existing first-party authentication flow through the system browser. API verification is isolated behind a provider interface and uses deterministic fixtures until E1-10 provisions live Google credentials. The provider interface supports a future Apple adapter without coupling session issuance to Google.

## Impact

- **Client:** Add a Google action to the welcome/sign-in surface, system-browser and deep-link bridge abstractions, safe cancellation/retry behavior, and privacy-safe interaction analytics.
- **API:** Add replay-protected OAuth transactions and verified provider-subject bindings; issue the existing session family after successful provider verification.
- **Engine / proto / infra:** No changes.
- **Data:** New provider bindings are backed up with Postgres and deleted with the Learner. OAuth attempts expire and contain no provider access or refresh credentials.

Relies on [ADR 0004](../../../adr/0004-auth.md), [ADR 0011](../../../adr/0011-owner-decisions-2026-11-09.md), and the learner-profile and analytics specifications.
