## 1. Tests first
- [x] 1.1 e2e `apps/client/e2e/social-e6-04.spec.ts`
- [x] 1.2 Postgres integration `block_learners_and_restrict_social_visibility_integration_test.go` (+ blocked-pair search/profile in E6-02 coverage)
- [x] 1.3 Go unit `block_learners_and_restrict_social_visibility_test.go` and Vitest `block_learners_and_restrict_social_visibility.test.tsx`

## 2. Implementation
- [x] 2.1 Migration: `learner_blocks`, `block_max`, `course_invites_from`
- [x] 2.2 Block, unblock, list; `BlockedEitherWay`, `CanInteract`, `CanInvite`, `NotBlockedSQL`, `OnBlock`
- [x] 2.3 Enforce blocks in search, profile, send, link; streak and policy in profile
- [x] 2.4 Analytics allowlist and catalogue
- [x] 2.5 Client: block confirm, Blocked list, invite setting
- [x] 2.6 Privacy classification, export, backup fixture, data inventory
