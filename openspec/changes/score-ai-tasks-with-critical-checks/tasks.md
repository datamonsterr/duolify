## 1. Tests (TDD - red first)
- [x] 1.1 Add e2e test `engine/crates/engine-eval/tests/score_each_ai_task_with_critical_checks_e2e.rs`.
- [x] 1.2 Add integration test `engine/crates/engine-eval/tests/score_each_ai_task_with_critical_checks_integration.rs`.
- [x] 1.3 Add unit tests in `engine/crates/engine-eval/src/main.rs #[cfg(test)]` and engine-eval modules.

## 2. Implementation
- [x] 2.1 Implement canonical task aliases, task registry, and extension registration.
- [x] 2.2 Implement per-task scoring and threshold validation from `evals/thresholds.toml`.
- [x] 2.3 Implement critical-check immediate failure logic.
- [x] 2.4 Implement dispatch for canonical tasks and extensions, enforcing feature gate on missing cases.
- [x] 2.5 Record task/dataset/model/prompt revisions in JSON and Markdown eval reports.
- [x] 2.6 Implement `eval_run_completed` analytics emitter with PostHog test-double assertion, opt-out, and privacy.
- [x] 2.7 Update golden fixtures in `evals/fixtures` with per-task threshold, critical failure, and rubric calibration cases.
- [x] 2.8 Update `docs/data-inventory.md` and CI workflow 90-day artifact retention.

## 3. Validation
- [x] 3.1 Verify `engine:lint`, `engine:test`, `engine:build`, `ci:lint`, `ci:openspec`, `engine:eval`.
