- [x] Add the pricing client acceptance test for a catalogue flip while a quote retains its original terms.
- [x] Add Postgres coverage for database-time resolution, concurrent reads, cancellation and rollback ancestry.
- [x] Add runtime reload fallback coverage and implement schedule, cancel, rollback, activation events and backup retention.
- [x] Run focused API and web client mise gates; run strict OpenSpec validation.

- [ ] Follow-up after issue #165 E12 scope: decide and implement a shared activation-state gate for `ActiveQuoteConfig` and other approved-version resolvers, including affected financial fixtures. Approval is now fail-closed with persisted actual verification evidence; quote selection currently still trusts prior approval if credentials/provider availability change afterwards.
