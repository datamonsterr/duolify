# Enrich notes with grounded summaries and tags (E9-02)

## Why
Learners need concise summaries, thematic tags, and grounded lesson links attached to their raw quick notes so that notes are easily reviewed, filtered, and connected to course content, without ever overwriting raw learner text or displaying hallucinated or foreign lesson links (#101).

## What Changes
- Add persisted `quick_note_enrichments` table in Postgres storing summary, tags, and validated lesson links with version, model, and provider metadata.
- Extend `EnrichNoteRequest` in `proto/duolify/engine/v1/engine.proto` with additive `repeated LessonRef allowed_lessons = 5;` field to ground lesson references against enrolled course context.
- Implement note enrichment logic in `engine-core` (`notes/mod.rs`), mock provider support in tests, timeouts/deadlines, and bounded fallback within quote caps.
- Enqueue River background worker job `note_enrich` to quote and reserve daily-first eligible credits (`quick_note` action from Pricing config), claim reservation, invoke `EnrichNote` over gRPC with allowed course lesson references, reject/drop foreign or hallucinated links, enforce summary/tag bounds, report actual usage and settle; release undelivered reservations on terminal failure, preserving raw text with status `enrichment_failed` or `raw_only`.
- Leave zero-balance notes in `enrichment_pending` without blocking note capture.
- Update Client (`NotesPage.tsx` and `QuickNotePanel.tsx`) matching `S21-Notes.dc.html` and `S09-Sheets.dc.html` with distinct original raw text vs AI additions, error recovery ("Retry enrichment · 2 credits"), empty, loading, long content, and desktop/mobile responsiveness.
- Add `quick_note_enriched`, `quick_note_enrichment_failed`, and `$ai_generation` to the analytics allowlist with strict privacy protection (no note text).
- Update account privacy export/deletion, backup fixtures, data inventory, and add eval golden set cases for `note_enrich` (cheap route, foreign link, empty/non-English profile, timeout).

## Impact
- Proto: `proto/duolify/engine/v1/engine.proto`, generated Go bindings in `services/api/gen/duolify/engine/v1/`.
- Engine: `engine/crates/engine-core/src/notes/`, `engine/crates/engine-server/src/lib.rs`, `engine/crates/engine-eval/src/runner.rs`, `evals/fixtures/golden.sample.jsonl`.
- API: `services/api/internal/notes`, `services/api/internal/worker`, `services/api/internal/analytics`, `services/api/internal/accounts/privacy.go`, `services/api/internal/backuptest/fixture.go`, `services/api/migrations/`.
- Client: `apps/client/src/features/notes/NotesPage.tsx`, `apps/client/src/features/notes/QuickNotePanel.tsx`, `apps/client/src/lib/analytics.ts`.
- Docs: `docs/data-inventory.md`.
