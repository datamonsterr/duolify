# Tasks: Handle exhausted retry pools without blocking tests

- [x] 1. Database schema and persistence
  - [x] 1.1 Create migration `20261110180000_lesson_test_variant_snapshots.sql` for variant provenance and instruction snapshots
  - [x] 1.2 Update `sqlc` queries in `services/api/queries/lessontest.sql`
  - [x] 1.3 Add table to `services/api/internal/accounts/privacy.go`, `docs/data-inventory.md`, and backup fixtures
- [x] 2. TDD tests (e2e -> integration -> unit)
  - [x] 2.1 e2e: `apps/client/e2e/lessontest-e4-07.spec.ts` for pool exhaustion, zero balance, failure recovery, keyboard and touch
  - [x] 2.2 integration: `services/api/internal/lessontest/handle_exhausted_retry_pools_without_blocking_tests_integration_test.go`
  - [x] 2.3 unit: `services/api/internal/lessontest/handle_exhausted_retry_pools_without_blocking_tests_test.go`, client vitest `apps/client/src/features/lessontest/handle_exhausted_retry_pools_without_blocking_tests.test.tsx`, engine cargo tests in `engine/crates/engine-core/src/lessontest/tests.rs`
- [x] 3. AI reliability & Golden set
  - [x] 3.1 Add `lesson_test_variant` exhaustion, failure, and paraphrase cases to `evals/fixtures/golden.sample.jsonl`
  - [x] 3.2 Verify `mise run engine:eval` passes with 100% threshold
- [x] 4. Implementation
  - [x] 4.1 Implement pool exhaustion free reuse, correct answer reordering, safe source fallback, and snapshot recording in `services/api/internal/lessontest/service.go`
  - [x] 4.2 Implement canvas S10-Test error state, recovery actions (Retry, Return to lesson), and banner in `apps/client/src/features/lessontest/LessonTest.tsx`
  - [x] 4.3 Add engine validation helpers in `engine/crates/engine-core/src/lessontest/mod.rs`
- [x] 5. Verification
  - [x] 5.1 Run all mise tasks: `ci:openspec`, `api:lint`, `api:test`, `api:build`, `client:lint-web`, `client:test-web`, `client:e2e`, `engine:test`, `engine:eval`
