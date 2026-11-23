## Implementation
- [x] Add failing Playwright e2e test for offline bookmarking, reconnecting twice, and deep link navigation.
- [x] Add failing Postgres integration test for target uniqueness, offline queue replay, and missing-target authorization.
- [x] Add failing Go and Vitest unit tests for boundary validation, target resolution, and event emitters.
- [x] Add Goose migration and sqlc queries for `bookmarks`.
- [x] Implement `services/api/internal/notes` bookmark service methods and HTTP handlers.
- [x] Update `services/api/internal/analytics` allowlist, client emitters, and analytics tests.
- [x] Implement client bookmarks feature, offline queue, BookmarksPage, and player tool integration.
- [x] Update privacy policy, backup fixtures, and data inventory.
- [x] Verify all checks pass across API and Client.
