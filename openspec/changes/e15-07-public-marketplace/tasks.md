## 1. Tests first (TDD)
- [x] 1.1 e2e `apps/client/e2e/sharing-e15-07.spec.ts`
- [x] 1.2 Integration `services/api/internal/sharing/browse_a_filtered_public_marketplace_integration_test.go`
- [x] 1.3 Unit `services/api/internal/sharing/browse_a_filtered_public_marketplace_test.go`, `apps/client/src/features/sharing/browse_a_filtered_public_marketplace.test.tsx`

## 2. Implementation
- [x] 2.1 Analytics allowlist and catalogue, then emitters
- [x] 2.2 Migration `20261115111507_marketplace_listings.sql`
- [x] 2.3 Listing, search, ranking, cursor, Redis cache, flag
- [x] 2.4 Routes, router public allowlist, enroll refactor
- [x] 2.5 Client Marketplace and Course page
- [x] 2.6 Data inventory, privacy policy, backup fixture, restore and deletion checks, `.env.example`
