# ADR 0020: Daily check-in Credits

- Status: Accepted (owner, 2026-11-22)
- Date: 2026-11-22

## Decision
The owner chose 150 Credits per daily attendance plus 250 once on the first check-in: 400 on day one. The regular bank cap is seven daily grants (1050); grant min(150, max(0, 1050 - banked)), then the first bonus in full. Banked includes held reservations. The bonus counts toward later caps; bonus/refund overflow is preserved. Sources never expire. Attendance differs from Lesson effort and does not affect XP or Streaks.

Keep the 46 Daily free credits unchanged. Spend eligible Daily free credits first, check-in Credits next, then existing oldest eligible paid sources. Check-in Credits fund all purchased-Credit actions. Record distinct grant/bonus ledger kinds with source-linked settlements and refunds.

Use an explicit authenticated idempotent app-open endpoint, not wallet-read/global middleware work. Persist attendance with a unique Learner/date key and frozen next-midnight eligibility boundary. Use the existing delayed effective profile zone; changes cannot advance the frozen boundary and previously claimed dates stay deduplicated. DST uses calendar midnights, never a fixed 24-hour cooldown. See the change design for westward date repetition and exact response semantics.

The owner approval authorizes exactly 150, 250 and 7 in an immutable approved Pricing successor of the merged approved Hy4 routing version. Copy all other terms and quote policy unchanged. No AI routing, price, pack, plan or limit changes are authorized by this decision.

## Consequences
The check-in grant is the separately budgeted onboarding subsidy chosen over shared-demo-only acquisition. Grants, attendance, transactional opt-out-aware analytics, export, erasure and backup classification ship together. No new model behavior or evaluation cases. Bonus uniqueness is per Learner account for life; deleting and re-registering the same email can claim again, an accepted documented abuse risk without new identity tracking.
