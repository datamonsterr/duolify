## 1. Tests first (TDD)
- [ ] 1.1 e2e `apps/client/e2e/sharing-e15-06.spec.ts`
- [x] 1.2 Integration `services/api/internal/sharing/invite_friends_to_courses_with_notifications_integration_test.go`
- [x] 1.3 Unit `services/api/internal/sharing/invite_friends_to_courses_with_notifications_test.go`, `apps/client/src/features/sharing/invite_friends_to_courses_with_notifications.test.tsx`

## 2. Implementation
- [x] 2.1 Analytics allowlist and catalogue, then emitters
- [x] 2.2 Migration `20261115121506_course_invites.sql`
- [x] 2.3 Invite service, outbox, push adapter, block callback, routes, wiring
- [ ] 2.4 Client invite sheet and received inbox
- [x] 2.5 Data inventory, privacy policy, backup fixture, restore and deletion checks, anonymize classification, `.env.example`
