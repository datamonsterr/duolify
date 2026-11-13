## 1. Tests first
- [ ] 1.1 e2e `apps/client/e2e/sharing-e15-01.spec.ts`
- [ ] 1.2 Postgres integration `preview_a_screened_publication_draft_integration_test.go` with real gRPC classifier
- [ ] 1.3 Go unit, Vitest and cargo `sharing/tests.rs`

## 2. Implementation
- [ ] 2.1 Proto `ScreenCourse`, regenerate Go and Rust bindings
- [ ] 2.2 Engine `sharing` module, mock provider, models, eval task and golden rows
- [ ] 2.3 Migration, snapshot builder, scanner, screening service, HTTP
- [ ] 2.4 Analytics allowlist and catalogue
- [ ] 2.5 Client preview panel, desktop and mobile
- [ ] 2.6 Privacy policy, backup fixture, data inventory, `.env.example`
