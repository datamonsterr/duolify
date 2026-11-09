## 1. E2E tests (write first, failing)

- [ ] 1.1 Add `rest of course` instruction, pass a lesson, next generated lesson reflects it (mock provider), instruction still active.
- [ ] 1.2 Add `next lesson` instruction, it applies once, status becomes `applied`, the following lesson does not use it.
- [ ] 1.3 List, edit, delete instructions; past lessons unchanged byte for byte.
- [ ] 1.4 Re-plan: quote shown, diff shown, confirm applies; discard keeps outline; stale proposal rejected.
- [ ] 1.5 Course copy of a published course: first instruction shows fork warning; after confirm the next lesson is generated for the Learner and charged; earlier reused lessons stay.
- [ ] 1.6 Injection instruction rejected with a reason; unsafe instruction rejected.
- [ ] 1.7 Insufficient credits on re-plan: refused, Engine not called.

## 2. Integration tests

- [ ] 2.1 Instruction store: limits (length, count, total, rate), scope transitions, soft delete.
- [ ] 2.2 Generation job snapshots instructions; retry reuses the snapshot; idempotent.
- [ ] 2.3 Re-plan apply is atomic and optimistic-locked; Engine response editing a generated lesson is rejected.
- [ ] 2.4 Credit reserve, settle, release for re-plan at the Pricing config version in the quote.
- [ ] 2.5 Fork bookkeeping on Course copy; reuse stops at `forked_at_lesson`.
- [ ] 2.6 Screening unavailable: instruction not saved (fail closed).
- [ ] 2.7 Account deletion removes instructions and proposals.

## 3. Unit tests

- [ ] 3.1 Scope consumption logic table.
- [ ] 3.2 Prompt builder: instructions only inside the delimited section; delimiter escaping.
- [ ] 3.3 Diff computation for outlines.
- [ ] 3.4 Deterministic screening rules.
- [ ] 3.5 Analytics emitters.

## 4. Implementation

- [ ] 4.1 Proto additions in `proto/` with matching `services/api` and `engine` changes in one PR (`buf lint`, `buf breaking`).
- [ ] 4.2 Migrations and API endpoints (`/v1/courses/{id}/instructions`, `/replan`, `/replan/{id}/confirm`).
- [ ] 4.3 Engine prompt section, `ReplanOutline`, `ScreenInstruction`.
- [ ] 4.4 Client panel, fork warning, diff view.

## 5. Evals, analytics, backup

- [ ] 5.1 Golden-set rows for `steer_lesson`, `steer_injection`, `steer_replan`, `steer_screen` in the Hugging Face dataset; thresholds in `evals/thresholds.toml`; pass `mise run engine:eval`.
- [ ] 5.2 Add events to the `analytics` catalog through its change-control rule.
- [ ] 5.3 Add steering tables to `docs/business/data-inventory.md`; confirm nightly dump and deletion path.
- [ ] 5.4 Confirm Pricing config key `outline_replan` approved before enabling re-plan (feature flag until then).
