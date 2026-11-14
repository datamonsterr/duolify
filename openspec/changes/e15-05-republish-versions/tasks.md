## 1. Tests first (TDD)
- [ ] 1.1 e2e `apps/client/e2e/sharing-e15-05.spec.ts`
- [ ] 1.2 Integration `services/api/internal/sharing/republish_versions_and_preserve_copy_progress_integration_test.go`
- [ ] 1.3 Unit `services/api/internal/sharing/republish_versions_and_preserve_copy_progress_test.go`, `apps/client/src/features/sharing/republish_versions_and_preserve_copy_progress.test.tsx`

## 2. Implementation
- [ ] 2.1 Analytics allowlist and catalogue, then emitters
- [ ] 2.2 Migration `20261115111505_republish_versions.sql`
- [ ] 2.3 Republish, lesson keys, update, archive, retention, routes, wiring
- [ ] 2.4 Client owner republish and learner update sheet
- [ ] 2.5 Data inventory, privacy, backup fixture, restore and deletion checks, anonymize classification, `.env.example`
