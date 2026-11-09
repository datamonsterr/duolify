## 1. Tests (TDD - red first)
- [x] 1.1 Add e2e test `engine/crates/engine-eval/tests/mirror_pinned_golden_revisions_for_offline_recovery_e2e.rs`.
- [x] 1.2 Add integration test `engine/crates/engine-eval/tests/mirror_pinned_golden_revisions_for_offline_recovery_integration.rs`.
- [x] 1.3 Add unit tests in `engine/crates/engine-eval/src/main.rs #[cfg(test)]` and engine-eval modules.

## 2. Implementation
- [x] 2.1 Update `services/api/internal/analytics/analytics.go` allowlist and analytics catalogue for `dataset_revision` and `dataset_source`.
- [x] 2.2 Implement SHA-256 checksumming and cache verification in `engine-eval`.
- [x] 2.3 Implement S3/RustFS mirror fallback with checksum and dataset validation in `fetch_dataset_with_mirror`.
- [x] 2.4 Enforce pinned 40-character hex revision validation and explicit failure on missing mirror.
- [x] 2.5 Record `dataset_source` and `dataset_revision` in `EvalReport` JSON and Markdown formats.
- [x] 2.6 Emit `eval_run_completed` analytics with `dataset_revision` and `dataset_source` properties.
- [x] 2.7 Add pinned source, failure, and mirror integrity golden set cases to `evals/fixtures/golden.sample.jsonl`.
- [x] 2.8 Update `docs/data-inventory.md`, `.env.example`, and ADR 0011 defaults.

## 3. Validation
- [x] 3.1 Verify `engine:lint`, `engine:test`, `engine:build`, `engine:eval`, `infra:lint`, `ci:openspec`.
