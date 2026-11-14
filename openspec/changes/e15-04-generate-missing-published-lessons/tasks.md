## 1. Tests first (TDD)
- [x] 1.1 e2e `apps/client/e2e/sharing-e15-04.spec.ts`
- [x] 1.2 Integration `services/api/internal/sharing/generate_missing_published_lessons_for_the_learner_integration_test.go`
- [x] 1.3 Unit `services/api/internal/sharing/generate_missing_published_lessons_for_the_learner_test.go`, `apps/client/src/features/sharing/generate_missing_published_lessons_for_the_learner.test.tsx`, `engine/crates/engine-core/src/sharing/tests.rs`

## 2. Implementation
- [x] 2.1 Analytics catalogue then emitter
- [x] 2.2 Migration `20261115071504_copy_generation_consents.sql`
- [x] 2.3 lessontest Prepare extraction, sharing missing-lesson service and routes
- [x] 2.4 Engine check, eval task, golden rows and thresholds
- [x] 2.5 Client quote sheet and path states
- [x] 2.6 Data inventory, backup fixture, restore and deletion checks
