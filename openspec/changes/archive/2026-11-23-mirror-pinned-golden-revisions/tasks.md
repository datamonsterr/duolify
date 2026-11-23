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

## 4. Issue #160 defect 5: repository-trusted dataset integrity
- [x] 4.1 Reproduce checksum bypasses with failing E10-04 e2e/integration tests and a promotion-gate unit test before implementation.
- [x] 4.2 Embed repository-reviewed dataset/revision SHA-256 pins; verify exact HF, mirror and cache bytes, reject unknown pins and missing cache/mirror checksums.
- [x] 4.3 Restrict HF mirror fallback to network/timeout/5xx; reject 401/403/404 and integrity errors without mirror access.
- [x] 4.4 Carry dataset verification into JSON/Markdown reports; fail promotion closed for unverified and legacy reports; update synthetic fixtures without weakening assertions.
- [x] 4.5 Document pin provisioning/recovery, backup/retention and unchanged content-free analytics in eval README and data inventory.
- [x] 4.6 Run engine:lint, engine:test, engine:eval, engine:build and ci:openspec with offline fake services only (114 workspace tests, 97 eval cases, 63 OpenSpec items passed).
