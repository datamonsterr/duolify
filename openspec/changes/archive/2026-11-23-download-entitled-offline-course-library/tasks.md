## 1. Acceptance and implementation
- [x] 1.1 Create and strictly validate the feature OpenSpec change (`mise run ci:openspec`).
- [x] 1.2 Write failing e2e test `apps/client/e2e/player-e3-06.spec.ts` for funded course library download, offline replay after subscription expiry, and canvas parity.
- [x] 1.3 Write failing integration test `services/api/internal/player/download_an_entitled_offline_course_library_integration_test.go` for Postgres entitlement authorization and download eviction.
- [x] 1.4 Write failing unit tests in `services/api/internal/player/download_an_entitled_offline_course_library_test.go` and `apps/client/src/features/player/download_an_entitled_offline_course_library.test.tsx`.
- [x] 1.5 Implement server-side entitlement check reusing E7 `credits.Service` and add course library download handler in API.
- [x] 1.6 Extend analytics allowlist and catalogue in `services/api/internal/analytics/analytics.go` and `apps/client/src/lib/analytics.ts`.
- [x] 1.7 Implement client course library download controller, storage controls, progress UI, canvas parity in `CourseDownload.tsx` / `LearnPath.tsx` / `cache.ts`.
- [x] 1.8 Run all tests, verify e2e, integration, unit tests pass, and update `docs/data-inventory.md`.
