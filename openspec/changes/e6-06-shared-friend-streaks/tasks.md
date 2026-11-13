## 1. Tests first
- [ ] 1.1 e2e `apps/client/e2e/social-e6-06.spec.ts`
- [ ] 1.2 Postgres integration `grow_and_protect_shared_friend_streaks_integration_test.go`
- [ ] 1.3 Go unit `grow_and_protect_shared_friend_streaks_test.go` and Vitest `grow_and_protect_shared_friend_streaks.test.tsx`

## 2. Implementation
- [ ] 2.1 Migration `20261115020605_friend_streaks.sql`
- [ ] 2.2 Pure evaluator, pair transaction, hooks, sweep
- [ ] 2.3 E5 `FreezeRules.Extra` and friend milestone grants
- [ ] 2.4 `removeFriendshipTx` ends the streak
- [ ] 2.5 Analytics allowlist and catalogue
- [ ] 2.6 Friends view and client UI
- [ ] 2.7 Privacy classification, export, backup fixture, data inventory
