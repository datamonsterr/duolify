## 1. Specs and docs
- [x] 1.1 Initialize OpenSpec with Claude Code integration
- [x] 1.2 Write product specs for 13 capabilities
- [ ] 1.3 Create `docs/data-inventory.md` from the specs (WP-DOCS)

## 2. Proto
- [ ] 2.1 buf lint, breaking check and codegen tasks

## 3. API
- [ ] 3.1 e2e test, integration test, unit test for health and engine client
- [ ] 3.2 Go scaffold, migrations, Dockerfile, `api:lint|test|build`

## 4. Engine
- [ ] 4.1 Tests for provider router and mock provider
- [ ] 4.2 Rust workspace, tonic server, eval binary, `engine:lint|test|build|eval`

## 5. Client
- [ ] 5.1 e2e, integration and unit tests for the player stub
- [ ] 5.2 Tauri 2 + React scaffold, `client:lint|test|build`

## 6. Infra and CI
- [ ] 6.1 docker compose with Postgres, Redis, RustFS (S3); Railway config; backup stub
- [ ] 6.2 Draft and ready pipelines with `ci-ok`

## 7. Cross-cutting
- [ ] 7.1 PostHog client wrapper with opt-out and event catalog test
- [ ] 7.2 `.env.example` entries for every new variable
