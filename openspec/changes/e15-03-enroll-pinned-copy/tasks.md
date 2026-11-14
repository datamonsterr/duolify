## 1. Tests first (TDD)
- [x] 1.1 e2e `apps/client/e2e/sharing-e15-03.spec.ts`
- [x] 1.2 Integration `services/api/internal/sharing/enroll_a_pinned_copy_and_reuse_lessons_for_free_integration_test.go`
- [x] 1.3 Unit `services/api/internal/sharing/enroll_a_pinned_copy_and_reuse_lessons_for_free_test.go` and `apps/client/src/features/sharing/enroll_a_pinned_copy_and_reuse_lessons_for_free.test.tsx`

## 2. Implementation
- [x] 2.1 Analytics catalogue then emitters
- [x] 2.2 Migration `20261115061503_course_copies.sql`
- [x] 2.3 Enroll service, route, unlock hook in lessontest
- [x] 2.4 Client enroll panel and path markers
- [x] 2.5 Data inventory, privacy, backup fixtures, restore/deletion checks, `.env.example`
