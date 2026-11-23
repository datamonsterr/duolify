## 1. Tests first
- [x] 1.1 Config parsing and session-TTL floor (unit)
- [x] 1.2 Older rows deleted, younger kept, batch loop terminates (integration)
- [x] 1.3 Concurrent sweeps delete each row once without error (integration)
- [x] 1.4 `created_at` indexes exist (integration)
- [x] 1.5 Worker registers one unique periodic job (unit)

## 2. Implementation
- [x] 2.1 `internal/retention` service and config
- [x] 2.2 River worker `row_retention`, wiring in `cmd/api`
- [x] 2.3 Migration with indexes
- [x] 2.4 `.env.example`, `docs/data-inventory.md`
