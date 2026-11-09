## 1. Implementation
- [ ] 1.1 Add failing e2e test in `apps/client/e2e/courses-e2-04.spec.ts`.
- [ ] 1.2 Add failing integration test in `services/api/internal/courses/recover_generation_and_remove_courses_integration_test.go`.
- [ ] 1.3 Add failing unit tests in `services/api/internal/courses/recover_generation_and_remove_courses_test.go`, `apps/client/src/features/courses/recover_generation_and_remove_courses.test.tsx`, and `engine/crates/engine-core/src/courses/tests.rs`.
- [ ] 1.4 Implement creation job recovery, SSE reconnect, terminal failure release and fresh quote retry in `services/api/internal/courses/creation.go`.
- [ ] 1.5 Implement owner-only course soft deletion and 30-day purge with unreferenced cleanup in `services/api/internal/courses/course_deletion.go` and router.
- [ ] 1.6 Add analytics allowlist and safe properties for `ui_recovery_selected` in `services/api/internal/analytics/analytics.go` and client analytics.
- [ ] 1.7 Update client creation step persistence and restoration across Brief, Preview, Outline and Create, and add course removal dialog to courses catalog.
- [ ] 1.8 Verify data inventory, backup fixtures, engine evals, and run focused mise tasks.
