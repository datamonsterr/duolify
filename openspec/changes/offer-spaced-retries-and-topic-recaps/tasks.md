## 1. Implementation
- [x] 1.1 Add failing e2e test `apps/client/e2e/lessontest-e4-03.spec.ts`.
- [x] 1.2 Add failing integration test `services/api/internal/lessontest/offer_spaced_retries_and_topic_recaps_integration_test.go`.
- [x] 1.3 Add failing unit tests `services/api/internal/lessontest/offer_spaced_retries_and_topic_recaps_test.go` and `apps/client/src/features/lessontest/offer_spaced_retries_and_topic_recaps.test.tsx`.
- [x] 1.4 Wire configuration settings (`min_gap`, `max_variants_per_topic`, `max_variants_per_test`) in config and service options.
- [x] 1.5 Implement server queue transitions, recap slide delivery, leave/resume trigger, and concurrent session safety.
- [x] 1.6 Update client LessonTest UI for taught-slide recap, leave/resume banner, accessible keyboard/focus transitions, and retry variant presentation.
- [x] 1.7 Add client analytics tracking for `ui_test_retry_presented` and `lesson_test_recap_shown`.
- [x] 1.8 Run focused mise tasks: `api:lint`, `api:test`, `api:test-integration`, `api:build`, `client:lint-web`, `client:test-web`, `client:build`, and `ci:openspec`.
