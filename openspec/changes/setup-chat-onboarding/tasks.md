# Tasks

- [ ] Write failing e2e tests in `apps/client/e2e/accounts-e1-11.spec.ts` for desktop and mobile setup chat, vague follow-up, summary editing, form fallback on failure, and single confirmation.
- [ ] Write failing integration tests in `services/api/internal/accounts/setup_chat_integration_test.go` for setup chat contract, confirm-only writes, opt-out, config-disabled fallback, and zero learner charges.
- [ ] Write failing unit tests in `services/api/internal/accounts/setup_chat_test.go`, `apps/client/src/features/accounts/setup_chat.test.tsx`, and `engine/crates/engine-core/src/accounts/tests.rs` for single follow-up, skipped fields, summary validation, bounded failures, and assistant announcements.
- [ ] Update `proto/duolify/engine/v1/engine.proto` with additive `SetupChat` RPC and `LearnerProfile` fields, and generate Go bindings.
- [ ] Implement `setup_chat` task in `engine/crates/engine-core/src/accounts/` and `engine-server`, update `models.toml`.
- [ ] Add `setup_chat` golden-set cases in `evals/fixtures/golden.sample.jsonl` and verify `mise run engine:eval`.
- [ ] Add versioned pricing key `setup_chat` in unapproved development config and pricing quote validation.
- [ ] Implement API endpoint `POST /v1/me/setup-chat` with engine client, rate/cap tracking, and analytics emission.
- [ ] Implement client setup chat UI matching `docs/design/canvas/S01-Welcome.dc.html` with mobile sheet, live summary editing, keyboard/accessibility, and form fallback.
- [ ] Update `docs/data-inventory.md` and record implementation defaults in `docs/adr/0011-owner-decisions-2026-11-09.md`.
- [ ] Verify all checks pass across areas.
