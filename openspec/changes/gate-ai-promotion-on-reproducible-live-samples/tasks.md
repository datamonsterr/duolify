# Tasks: Gate AI promotion on reproducible live samples (E10-06)

## 1. Specification and TDD Scaffolding
- [x] 1.1 Create OpenSpec change proposal, design, and specification updates.
- [x] 1.2 Write failing e2e tests in `engine/crates/engine-eval/tests/gate_ai_promotion_on_reproducible_live_samples_e2e.rs`.
- [x] 1.3 Write failing integration tests in `engine/crates/engine-eval/tests/gate_ai_promotion_on_reproducible_live_samples_integration.rs`.
- [x] 1.4 Write failing unit tests in `engine/crates/engine-eval/src/main.rs #[cfg(test)]` and modules.

## 2. Implementation
- [x] 2.1 Implement task-stratified bounded sample selection in `engine-eval`.
- [x] 2.2 Implement provider-budget accounting and budget halting in `engine-eval`.
- [x] 2.3 Implement explicit missing-secret status and CLI ready mode in `engine-eval`.
- [x] 2.4 Extend evaluation report contract (case, revision, provider, usage, budget, thresholds) in JSON and Markdown.
- [x] 2.5 Implement feature promotion eligibility verification.
- [x] 2.6 Enforce critical safety, ledger, and cost check rejections.

## 3. Verification & Documentation
- [x] 3.1 Pass all e2e, integration, and unit tests without network or paid calls.
- [x] 3.2 Update `evals/fixtures/golden.sample.jsonl` with stratified sample and budget/cost cases.
- [x] 3.3 Update `docs/data-inventory.md`, `.env.example`, and ADR 0011 defaults.
- [x] 3.4 Run `engine:lint`, `engine:test`, `engine:eval`, `engine:build`, `ci:lint`, and `ci:openspec`.
