## 1. Acceptance and implementation
- [ ] 1.1 Create and strictly validate the feature OpenSpec change (`mise run ci:openspec`).
- [ ] 1.2 Write failing e2e test `apps/client/e2e/player-e3-05.spec.ts` for reduced motion and missing asset recovery.
- [ ] 1.3 Write failing integration test `services/api/internal/player/provide_accessible_static_lesson_recovery_integration_test.go`.
- [ ] 1.4 Write failing unit tests in `services/api/internal/player/provide_accessible_static_lesson_recovery_test.go` and `apps/client/src/features/player/provide_accessible_static_lesson_recovery.test.tsx`.
- [ ] 1.5 Implement manifest explanation contract in API (`manifest.go`, `player.go`, `fixture.go`) and client (`manifest.ts`).
- [ ] 1.6 Extend analytics catalogue and allowlist in `services/api/internal/analytics/analytics.go` and `apps/client/src/lib/analytics.ts`.
- [ ] 1.7 Implement static frame, full explanation, missing asset stage, recovery actions, and disabled advancement in `LessonPlayer.tsx` and design tokens/styles.
- [ ] 1.8 Verify all tests pass, data inventory is updated, and run mise lint/test/build tasks.
