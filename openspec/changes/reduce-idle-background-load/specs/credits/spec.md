## ADDED Requirements

### Requirement: Credit maintenance timing
Background credit maintenance (abandoned-hold recovery, payment inbox work, outbox delivery, daily expiry) SHALL run only in the worker process, never in API replicas. When idle it SHALL poll no more often than every 5 seconds and back off toward 30 seconds. It MUST read work through indexed due predicates and MUST NOT hold the global financial lock across a scan of many learners: expiry takes the lock once per learner, for that learner's own rows. Ledger semantics are unchanged: reservations, settlement, daily grants (including the lazy grant on a learner's first request of the local day), expiry and annual allowances behave as before.

#### Scenario: Two time zones cross local midnight
- **WHEN** one learner is in Pacific/Auckland and another in America/Los_Angeles, both holding an unspent daily grant for the same UTC instant
- **THEN** each learner's grant expires once, after that learner's own local midnight, and never twice

#### Scenario: Two workers sweep together
- **WHEN** two worker processes run expiry or outbox delivery at the same moment
- **THEN** each expiry row is written once and each outbox event is delivered once

#### Scenario: Wallet read
- **WHEN** a learner reads their wallet
- **THEN** only that learner's past-day grants are expired, not every learner's

#### Scenario: Idle system
- **WHEN** no work is due
- **THEN** an empty outbox costs one indexed probe and no transaction, and one worker plus one API replica send at most 150 statements a minute
