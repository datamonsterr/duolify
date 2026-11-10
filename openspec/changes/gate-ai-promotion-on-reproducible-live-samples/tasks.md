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

## 4. Review Defect Remediation (E10)
- [x] 4.1 Measure live eval budget summing primary and judge provider usage via configured price table (`prices.toml`), failing closed on usage_unavailable.
- [x] 4.2 Enforce at most one fallback after primary (max 2 calls total) in `ProviderRouter::complete` and `stream`.
- [x] 4.3 Route `enrich_note` and eval runner through `run_bounded` with limits, returning real attempts.
- [x] 4.4 Implement `duolify-eval gate <report.json>` subcommand and function requiring all canonical tasks with minimum per-task coverage (`total_cases >= 1`).
- [x] 4.5 Enforce minimum per-task coverage on sampled runs (`EVAL_SAMPLE_LIMIT`).
- [x] 4.6 Strict judge verdict parsing with word boundaries and structured JSON.
- [x] 4.7 Missing-secrets reports emit provider "none" and revision "none", without bypassing secret checks when EVAL_DATA_PATH is set.
- [x] 4.8 Require explicit `HF_GOLDEN_REVISION` in `.env.example` and CI (no `main` fallback; missing reports missing-config status).
- [x] 4.9 `stream_bounded` honors cost and deadline and falls back on first-token timeout; error on `--mode draft` with `--provider live`.
- [x] 4.10 Analytics `eval_run_completed` includes provider, status, and budget with structural privacy guards.
