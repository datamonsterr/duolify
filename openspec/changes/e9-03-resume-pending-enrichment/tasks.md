## Implementation
- [ ] Add failing Playwright e2e test (`apps/client/e2e/notes-e9-03.spec.ts`) exhausting credits, granting next day, and verifying oldest notes enriched first.
- [ ] Add failing integration test (`services/api/internal/notes/resume_pending_enrichment_when_credits_arrive_integration_test.go`) covering Postgres queue locks, grant outbox wakeup, raw revision conflicts, and provider failure.
- [ ] Add failing unit tests (`services/api/internal/notes/resume_pending_enrichment_when_credits_arrive_test.go`, `apps/client/src/features/notes/resume_pending_enrichment_when_credits_arrive.test.tsx`, and `engine/crates/engine-core/src/notes/tests.rs`).
- [ ] Add Goose migration and sqlc queries for `quick_notes.raw_version` and pending lookup index.
- [ ] Implement pending enrichment resumption and credit wakeup mechanism in API.
- [ ] Implement raw revision conflict checking and deduplication in note enrichment worker.
- [ ] Update client notes UI and recovery actions matching design canvases S21 and S09.
- [ ] Add Golden set eval rows in `evals/fixtures/golden.sample.jsonl` and ensure `mise run engine:eval` passes.
- [ ] Verify all mise tasks pass.
