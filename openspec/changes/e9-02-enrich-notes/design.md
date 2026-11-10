# Design: Enrich notes with grounded summaries and tags (E9-02)

## Context
E9-01 implemented raw quick note capture and offline synchronization. E9-02 implements asynchronous AI enrichment through River workers: quoting and reserving credits via the Pricing config (`quick_note` action), calling `EnrichNote` gRPC with allowed course lesson references, validating and grounding outputs, storing summary, tags, and links in `quick_note_enrichments`, settling upon delivery, and releasing reservations on failure while keeping learner raw text intact.

## Architecture and Data Model

### 1. Database Schema
- **`quick_note_enrichments` Table**:
  - `id uuid PRIMARY KEY DEFAULT gen_random_uuid()`
  - `note_id uuid NOT NULL REFERENCES quick_notes(id) ON DELETE CASCADE`
  - `learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE`
  - `summary text NOT NULL`
  - `tags text[] NOT NULL DEFAULT '{}'`
  - `links text[] NOT NULL DEFAULT '{}'`
  - `version integer NOT NULL DEFAULT 1`
  - `model text NOT NULL DEFAULT ''`
  - `provider text NOT NULL DEFAULT ''`
  - `created_at timestamptz NOT NULL DEFAULT now()`
  - `updated_at timestamptz NOT NULL DEFAULT now()`
  - `UNIQUE(note_id)`
  - Index on `quick_note_enrichments(learner_id)`
- **`quick_notes` Updates**:
  - Add optional columns `reservation_id uuid REFERENCES credit_jobs(id) ON DELETE SET NULL` and `quote_id uuid REFERENCES pricing_quotes(id) ON DELETE SET NULL`.
  - Status transitions: `enrichment_pending` -> `enriching` -> `enriched` or `enrichment_failed` (or `raw_only`).

### 2. Proto Contract
- Extend `EnrichNoteRequest` in `proto/duolify/engine/v1/engine.proto`:
  ```protobuf
  message EnrichNoteRequest {
    string note = 1;
    LessonRef lesson = 2;
    LearnerProfile learner = 3;
    UsageLimits limits = 4;
    repeated LessonRef allowed_lessons = 5;
  }
  ```
- Regenerate Go bindings via `mise run proto:gen`.

### 3. Engine Core and Server
- Implement `engine_core::notes`:
  - `prompt(&note, lesson, learner, allowed_lessons)`: formats prompt instructing model to summarize, tag, and select relevant lesson references strictly from `allowed_lessons`.
  - `validate_enrichment(text)`: validates JSON output schema containing `summary`, `tags`, and `links`.
  - `enrich(router, note, lesson, learner, allowed_lessons, limits)`: runs validated execution with attempt/USD caps, enforces summary/tag bounds, and filters out any foreign or hallucinated lesson links not in `allowed_lessons`.
- In `engine-server`, implement `enrich_note` RPC using `engine_core::notes::enrich`.

### 4. River Worker and API Service
- Worker job `note_enrich`:
  1. Takes `NoteEnrichArgs{NoteID: uuid, LearnerID: uuid}`.
  2. Quotes action `quick_note` via `pricing.NewQuotes(pool).Create(ctx, learnerID, QuoteInput{Action: "quick_note"})`.
  3. Accepts quote and reserves credits via `credits.ReserveAction(ctx, learnerID, quoteID, "quick_note")`.
     - If balance insufficient, leaves status `enrichment_pending` and exits cleanly.
  4. Fetches note raw text and all lesson stubs for the course (`allowed_lessons`).
  5. Claims reservation via `credits.Claim(ctx, learnerID, reservationID)`.
  6. Calls `EnrichNote` gRPC with deadline from reservation.
  7. Validates response, rejects foreign lesson links, enforces summary length and tag count bounds.
  8. Settles reservation via `credits.Settle(ctx, learnerID, reservationID, attempts)`.
  9. Upserts `quick_note_enrichments` and sets note status to `enriched`.
  10. Emits `quick_note_enriched` and `$ai_generation`.
  11. On terminal failure: releases reservation via `credits.Release`, leaves raw text intact, sets status `enrichment_failed`, emits `quick_note_enrichment_failed` and `$ai_generation`.

### 5. Client UI Parity
- Matches `S21-Notes.dc.html` and `S09-Sheets.dc.html`:
  - Distinct sections for "Your original note" (raw text, source info) and "AI additions" (summary, `#tag` badges, "Related: [title]" lesson links).
  - Enrichment states: `enrichment_pending`, `enriching`, `enriched`, `enrichment_failed`, and `waiting_to_sync`.
  - Recovery on failure: alert banner "Couldn't add the AI summary. Your note is safe. No credits were charged." with "Retry enrichment · 2 credits" button.
  - Mobile responsiveness: list and detail views render as two distinct screens with navigation back button.

### 6. Analytics and Privacy
- Add `quick_note_enriched` and `quick_note_enrichment_failed` to `allowedEvent` and `safeProperties` in `services/api/internal/analytics`.
- Never include raw note text or AI summary in PostHog payloads.
- Update `ClassifiedLearnerTables` and `exportAccountData` in `services/api/internal/accounts/privacy.go` to include `quick_note_enrichments`.
- Update backup fixture in `services/api/internal/backuptest/fixture.go` and `docs/data-inventory.md`.
## Review corrections: learner ownership

Enrichment entry points and River payloads carry the authenticated learner ID. Reads, status updates and locking reads require both note and learner IDs; a foreign note returns 404 before creating a quote or reservation. Legacy jobs without a learner ID fail closed. This preserves the existing backup inventory, analytics privacy policy and mock-provider reliability coverage.

Non-delivery settles an explicit zero-cost, non-delivered attempt, including after Claim; nil usage retains its shared meaning of unknown provider cost. Cleanup uses a bounded context independent of request cancellation and propagates settlement and status errors. Accepted enrichment, raw-version validation and final status commit together with the ledger through SettleWith. Mock-provider regression cases assert zero ledger delta for provider errors, rejected output, concurrent edits and failed database writes, and one charge for successful delivery.

A conditional UPDATE RETURNING claims a pending or failed raw revision before quoting. Competing enrichment entry points yield to that claim. River uniqueness uses note ID and raw version; saving an idempotent replay returns the existing note without inserting another job. A new raw note and its River job commit in one transaction; queue failures are returned and roll back the insert. Fixtures migrate River alongside the application schema.

Course context is derived from an authorized lesson or checked against ownership/enrollment. A supplied lesson and course must match. Workers recheck persisted references before quoting; allowed lessons are learner-scoped, and context-free notes have no arbitrary enrollment fallback. Foreign context never reaches the engine or stored links.

