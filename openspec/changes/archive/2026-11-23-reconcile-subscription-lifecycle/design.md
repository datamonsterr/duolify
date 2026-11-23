# Design and conservative defaults

The existing credit maintenance worker reconciles on its first minute and daily thereafter, retrying provider failures on the next minute. Canonical latest invoice references re-enter durable invoice verification; status alone never funds.

Canonical retrieval happens under the existing financial transaction lock. Append-only observations are deduplicated by canonical status/period/cancel state; the newest observation supplies status while funded terms alone authorize benefits. Trial/incomplete/paused never create funding. Unknown statuses fail closed. Ordinary expiry is derived at read/claim time so Redis outages cannot extend access.

Plan changes pin the active Pricing version and configured provider mapping. Only one pending schedule is accepted per funded renewal boundary; same intent retries are idempotent. Same billing interval only at launch (interval switches require separately verified billing anchors). Fixture scheduling preserves the current phase and applies a future phase at the paid-through boundary with no proration. No live adapter or native implementation is added. Full refund/dispute uses existing revocation history; monetary reversals remain E7-10.

Priority uses the configured `Priority generation queue` benefit in the immutable funded term. Existing reservations and USD guards are acquired before claim; claim-time priority ordering never increases those bounds. Client requests cannot set priority. Waiting work is retried by its caller; existing synchronous generation boundary remains.

Official references checked 2026-11-09: https://docs.stripe.com/billing/subscriptions/webhooks (all eight statuses and dunning), https://docs.stripe.com/billing/subscriptions/prorations, https://docs.stripe.com/billing/subscriptions/subscription-schedules (both proration settings and next-phase timing). No new model behavior; existing cost/evaluation/fallback gates stay in force.
