# Apple OAuth sign-in

Add Apple sign-in to the existing first-party authentication flow through the system browser. API verification is isolated behind the provider interface and uses deterministic fixtures until E1-10 provisions live Apple credentials. Apple identity is keyed by its stable subject; first-login name and email claims are optional on later callbacks and never used to infer account linking.

## Impact

- **Client:** Add an Apple action to the existing Welcome/sign-in surface and reuse the Google system-browser and deep-link bridge, cancellation, focus recovery, session storage, and privacy-safe analytics behavior.
- **API:** Add Apple-specific verified claim handling, including issuer, audience, nonce and signing-key rotation validation at the provider boundary, and bind the stable Apple subject into the existing provider identity table.
- **Data:** Existing provider binding rows are already backed up and cascade with Learner deletion. A returning Apple subject can sign in without a repeated email or name claim.
- **Live credentials:** No Apple client secret or live adapter is included. E1-10 owns provisioning.
- **Engine / proto / infra:** No changes.

Relies on [ADR 0004](../../../adr/0004-auth.md), [ADR 0011](../../../adr/0011-owner-decisions-2026-11-09.md), and the learner-profile and analytics specifications.
