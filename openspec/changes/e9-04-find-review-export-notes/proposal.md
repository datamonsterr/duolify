# Find review and export quick notes (E9-04)

## Why
Learners need a reliable note library that works completely independent of AI availability (#103). Notes must be discoverable via search and filters (by course, lesson, status, tags, and review state), editable with optimistic revision conflict detection so offline edits or multi-device edits do not silently overwrite each other, markable as reviewed, deletable, and exportable to cleanly formatted Markdown separating original learner words from AI enrichments.

## What Changes
- Add support for optimistic concurrency versioning and reviewed timestamps on quick notes.
- Implement scoped search and multi-facet filtering (course, lesson, tag, review state, text query) for learner quick notes on the API.
- Implement raw text editing endpoint with optimistic revision conflict detection (409 Conflict when expected version mismatches) that preserves prior enrichment intact.
- Implement mark reviewed endpoint and note deletion endpoint, respecting privacy and owner scoping.
- Implement Markdown export of single notes and note libraries with distinct sections for "Your original note" and "AI additions".
- Add analytics catalogue events: `quick_note_reviewed`, `quick_note_deleted`, and `ui_control_activated` with `control_id=export_notes`, enforcing strict privacy protection (never sending note text or search queries).
- Implement desktop list-detail layout and mobile two-screen flow matching canvas `docs/design/canvas/S21-Notes.dc.html` with full loading, empty ("Keep an idea from your next lesson"), no-results ("Clear filters"), offline/error, edit conflict resolution ("Keep selected"), keyboard/focus, light/dark, and reduced motion states.
- Update data inventory and privacy policies.

## Impact
- API: `services/api/internal/notes`, `services/api/internal/analytics`, `services/api/internal/accounts/privacy.go`, `services/api/internal/backuptest/fixture.go`.
- Client: `apps/client/src/features/notes` (`NotesPage.tsx`), `apps/client/src/lib/analytics.ts`.
- Documentation: `docs/data-inventory.md`.
- No new AI behavior introduced.
