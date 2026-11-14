## 1. Tests first (TDD)
- [x] 1.1 e2e `apps/client/e2e/sharing-e15-02.spec.ts`
- [x] 1.2 Integration `services/api/internal/sharing/publish_revocable_links_with_public_previews_integration_test.go`
- [x] 1.3 Unit `services/api/internal/sharing/publish_revocable_links_with_public_previews_test.go` and `apps/client/src/features/sharing/publish_revocable_links_with_public_previews.test.tsx`

## 2. Implementation
- [x] 2.1 Migration `20261115051502_publication_links.sql`
- [x] 2.2 Publish service: token generation, hash storage, visibility update, analytics
- [x] 2.3 Revoke/rotate: immediate effect, no cache, read-through
- [x] 2.4 Public preview endpoint: allowlisted content, noindex, per-IP rate limit
- [x] 2.5 Client: visibility panel, link display, copy/revoke/rotate, preview page
- [x] 2.6 Data inventory, privacy policy, backup fixtures, .env.example
