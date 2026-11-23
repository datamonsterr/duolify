# Tasks: Resume and Replay Lesson Test Sessions

- [x] Write failing e2e test `apps/client/e2e/lessontest-e4-04.spec.ts` <!-- id: 0 -->
- [x] Write failing integration test `services/api/internal/lessontest/resume_and_replay_lesson_test_sessions_integration_test.go` <!-- id: 1 -->
- [x] Write failing unit tests `services/api/internal/lessontest/resume_and_replay_lesson_test_sessions_test.go` and `apps/client/src/features/lessontest/resume_and_replay_lesson_test_sessions.test.tsx` <!-- id: 2 -->
- [x] Create database migration `20261110170000_lesson_test_answer_operations.sql` and run `mise run api:generate` <!-- id: 3 -->
- [x] Register `lesson_test_answer_operations` in `privacy.go`, `fixture.go`, and `data-inventory.md` <!-- id: 4 -->
- [x] Implement backend session TTL, resume, operation key idempotency, and unserved key guard in `service.go` and `config.go` <!-- id: 5 -->
- [x] Implement client offline queuing, replay sync, and canvas S10/S12 states in `LessonTest.tsx` <!-- id: 6 -->
- [x] Verify all tests pass across e2e, integration, and unit suites <!-- id: 7 -->
