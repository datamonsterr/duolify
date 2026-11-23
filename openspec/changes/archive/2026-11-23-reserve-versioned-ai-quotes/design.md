## Transaction boundary

The shared credits service snapshots no new commercial terms: it reads the
immutable quote and its consent, then holds eligible source entries, operational
USD and a job in one Postgres transaction. A transaction advisory lock serializes
financial writes, including expiry and settlement, so platform counters and
learner allocations have one lock order across API processes. Provider work runs
outside that transaction only after an atomic claim. Postgres is authoritative;
Redis wallet entries and analytics are disposable projections.

Daily sources are selected first only for eligible actions. Paid sources follow
issuance order. Each component allocation retains its source, grant date and
original quote version. Finalization appends a full hold reversal and actual
spend in one transaction, then immediately expires unused old-day daily funds.
This preserves the existing ledger balance and wallet reservation derivation.
Content minimum/ceiling and aggregate rational audio rounding run once per job.
An opaque delivered artifact key deduplicates component charges across jobs.

## Execution and failure

The API replaces caller-supplied bounds with quote-owned token, time, attempt and
USD bounds and verifies the feature/action before claiming the job. The private
preview RPC verifies the configured route and runs the bounded provider pipeline;
its complete attempt usage is priced by the API. Other feature implementations
consume the same ReserveAction/Limits/Claim/Settle interface; this change does
not enable new generation features or live model promotion.

One primary attempt and one evaluated fallback fit within the configured total
cap. Invalid or missing started-job usage retains a conservative operational USD
hold as spent, releases undelivered customer Credits, and remains auditable.
Expired unclaimed jobs release both holds. Deadline cleanup and financial outbox
processing run in API and worker processes and survive restart through Postgres.
Credit events are recorded once per operation. The nonblocking analytics adapter
is a best-effort projection, with learner/global opt-out and no learner text.

## Version activation and recovery

A successor catalogue owns the five-minute TTL and free concurrency default;
historical catalogue rows are unchanged. New admin approvals copy their parent's
execution policy, including price rollback, while quote activation excludes
cancelled versions. Existing accepted jobs retain their original policy and caps.
New grants resolve the active pricing version rather than a startup copy.

All six financial tables are included in account export and retained under
pseudonymous ownership after account deletion. Backup fixtures contain both a
settled job and an outstanding reservation and compare complete restored rows,
provider counters, source allocations and ledger balances. Offline golden cases
exercise the production usage pipeline; Postgres tests cover version flips,
midnight, concurrency and caps. Live HF and model promotion stay gated by ADR 0011.
