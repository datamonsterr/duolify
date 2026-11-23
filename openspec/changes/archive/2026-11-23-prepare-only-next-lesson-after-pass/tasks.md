# Tasks: Prepare only the next lesson after a pass

## 1. Tests (TDD Order)
- [x] 1.1 Create e2e test in `apps/client/e2e/lessontest-e4-05.spec.ts` (Playwright) asserting pass N, worker restart, only N+1 generated, and S12/S08 desktop/mobile states.
- [x] 1.2 Create integration test in `services/api/internal/lessontest/prepare_only_the_next_lesson_after_a_pass_integration_test.go` asserting Postgres completion/outbox/queue transaction, quote reuse, predecessor verification, and history ordering.
- [x] 1.3 Create unit tests in `services/api/internal/lessontest/prepare_only_the_next_lesson_after_a_pass_test.go`, `apps/client/src/features/lessontest/prepare_only_the_next_lesson_after_a_pass.test.tsx`, and `engine/crates/engine-core/src/lessontest/tests.rs`.

## 2. Database & API
- [x] 2.1 Add Goose migration `20261110160000_lesson_completion_summaries.sql` and run `mise run api:generate`.
- [x] 2.2 Update `services/api/internal/accounts/privacy.go`, `services/api/internal/backuptest/fixture.go`, and `docs/data-inventory.md`.
- [x] 2.3 Implement completed-lesson summaries persistence and weak topics extraction upon pass.
- [x] 2.4 Implement strict predecessor pass check on prepare endpoint (refuse locked predecessor without pass).
- [x] 2.5 Populate `CourseOutline`, `LearnerProfile`, and completed lesson `history` (oldest first) in `RunLessonGeneration`.
- [x] 2.6 Ensure generation failure or insufficient credits does not undo completed lesson.

## 3. Client UI & Canvas Parity
- [x] 3.1 Update `LessonTest.tsx` finish screen to render S12-Finish canvas states (ready, needs credits, failed retry) with keyboard/touch parity.
- [x] 3.2 Update `LearnPath.tsx` to display ready, preparing, insufficient credits, and failure retry states matching S08-Path canvas.

## 4. AI Reliability & Analytics
- [x] 4.1 Add golden-set rows in `evals/fixtures/golden.sample.jsonl` testing history and empty-profile cases; verify `mise run engine:eval`.
- [x] 4.2 Verify allowlisted analytics events and ensure zero learner text is emitted.
