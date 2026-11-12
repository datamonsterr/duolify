## Implementation
- [ ] Add failing Playwright e2e test for filter, edit, review, conflict resolution, export and delete notes (`apps/client/e2e/notes-e9-04.spec.ts`).
- [ ] Add failing Postgres integration test for scoped search/review state, revision conflict and Markdown export (`services/api/internal/notes/find_review_and_export_quick_notes_integration_test.go`).
- [ ] Add failing unit tests for Go API notes service (`services/api/internal/notes/find_review_and_export_quick_notes_test.go`) and Vitest client (`apps/client/src/features/notes/find_review_and_export_quick_notes.test.tsx`).
- [ ] Add database migration for `quick_notes.version` and `quick_notes.reviewed_at`.
- [ ] Implement sqlc queries and Go API methods for search/filters, optimistic update with conflict detection, mark reviewed, delete note, and Markdown export.
- [ ] Add analytics events `quick_note_reviewed`, `quick_note_deleted`, and `ui_control_activated(export_notes)` to allowlist, sanitizer, and tests.
- [ ] Implement client `NotesPage.tsx` updates for multi-facet filtering, text search, inline editing, revision conflict resolution, reviewed toggle, deletion, and Markdown export matching desktop list-detail and mobile two-screen states.
- [ ] Update data inventory, privacy policy, and backup fixtures.
- [ ] Verify all mise tasks pass (`api:lint`, `api:test`, `api:build`, `api:test-integration`, `api:test-e2e`, `client:lint-web`, `client:test-web`, `client:build`, `client:e2e`, `ci:openspec`).
