## 1. Tests first
- [x] 1.1 e2e: restore a synthetic dump and fail an injected corruption (`backup_e11_05_test.go`)
- [x] 1.2 integration: RustFS + Postgres 18 isolated restore, migration registry, anonymization fixtures
- [x] 1.3 unit: newest dump, RPO/RTO bounds, events, opt-out, disabled scheduled drill

## 2. Implementation
- [x] 2.1 Drill runner, checks registry, anonymizer, Docker throwaway restorer
- [x] 2.2 `cmd/restore-drill`, `api:restore-drill-ci`, `infra:restore-drill-scheduled`
- [x] 2.3 CI job wired into `ci-ok`; scheduled workflow
- [x] 2.4 Docs, `.env.example`, data inventory
