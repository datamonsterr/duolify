- [x] Add the pricing client acceptance test for a catalogue flip while a quote retains its original terms.
- [x] Add Postgres coverage for database-time resolution, concurrent reads, cancellation and rollback ancestry.
- [x] Add runtime reload fallback coverage and implement schedule, cancel, rollback, activation events and backup retention.
- [x] Run focused API and web client mise gates; run strict OpenSpec validation.

- [x] Follow-up after issue #165 E12 scope: shared activation-state gate for `ActiveQuoteConfig`. Decision: no new quote-time re-verification. `ActiveQuoteConfig` already re-checks approval, cancellation and the activation marker on every quote, and the marker is written only by `ActivateDue` after the external mapping verifier passed, so a quote never resolves to an unapproved, cancelled or unverified version. Re-running provider verification per quote would put external calls and their outages on the quote path and fail every paid action when a provider is down; the operator control for a bad version is cancellation or a forward rollback version, both of which quote time honours. Revisit if verification evidence gains an expiry.
