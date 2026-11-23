- [x] E2E first: settings export, deletion confirmation, successful deletion and recovery states on desktop and mobile.
- [x] Integration second: export coverage for all learner-linked tables, session revocation, pseudonymous ledger retention, tombstone replay and restore checks.
- [x] Unit third: export filtering/encryption boundaries, deletion confirmation, client cache clearing and analytics opt-out.
- [x] Update analytics catalogue, backup fixture, data inventory and account privacy controls.
- [x] Run focused API/client mise gates and strict OpenSpec validation; record native Tauri checks as skipped on this host.

## Erasure coverage (fix/account-erasure-coverage)

- [x] PostHog person deletion was queued but never executed: River job `posthog_person_deletion` (every 10 minutes, `PostHogPersonEraser`), attempts counted, "person not found" is success, not scheduled without `POSTHOG_PROJECT_ID` and `POSTHOG_PERSONAL_API_KEY`, failures logged with counts only. Migration `20261123100000_posthog_person_deletion_attempts.sql`.
- [x] Integration (Postgres + fake PostHog): erasure queues one request; outage keeps and counts it; recovery drains it; repeat is a no-op; "not found" is success; nothing is called without config. Worker unit tests: scheduled only when configured.
- [x] Account erasure and tombstone replay ledger the learner's private generated-lesson prefixes in `course_object_purges` in the erasure transaction (`accounts.QueuePrivateObjectPurges`); the API object sweep deletes them after 15 days unless still referenced.
- [x] Integration: erase with a generated course; inside the window nothing is deleted; a storage failure leaves the row pending; the retry deletes only the private prefix; published snapshot, narration and rig objects and another learner's prefix stay; a second run is a no-op; tombstone replay ledgers too.
- [x] Account data export stores no object (response only, client-side encrypted); nothing to expire. `docs/data-inventory.md` lists every object class with its erasure behaviour.
- [ ] Owner decision: see the open object classes in the PR description (none block this change).
- [x] PostHog retries use bounded backoff and do not starve new requests; worker receives private deletion credentials.
