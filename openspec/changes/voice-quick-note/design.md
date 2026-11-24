# Design

## Flow
1. Press (or `startVoiceQuickNote`): capture `{courseId, lessonId, slideIndex, slideTitle, playbackMs, rect?}` from the player view and pause if playing (remember `pausedByUs`). Emit `voice_quick_note_started`.
2. Recording uses `VoiceBubble` unchanged: hold to talk, tap toggle, slide-off or Escape cancels (`voice_quick_note_cancelled`).
3. Release: transcribe with the existing voice endpoint, then save through the same `noteQueue.enqueue` path `QuickNotePanel` uses for a dictated note, anchor `input: "voice"`. Offline uses the existing queue.
4. Toast "Saved" with Undo; Undo removes the queued or server note (`voice_quick_note_undone`). Resume playback only if `pausedByUs` and still paused.
5. Failures reuse the existing voice toasts: too short, nothing understood, offline, daily cap, microphone denied. `voice_quick_note_failed` carries the reason.

## Where the code lives
A headless `useVoiceQuickNote` hook in `features/notes`, extracted from the dictated-note branch of `QuickNotePanel` (`handleSave(dictation)` and `undoDictation`) so the panel and the hook share one save and undo path. The panel keeps working for the typed note. The rail button and the player control are thin wrappers. New code is lazy-imported so the initial chunk budget is not touched.

## Formatting
`note_enrich` today returns summary, tags and links from the note text and a Lesson reference. It gains Course, Lesson and Slide context and returns `title` and `formatted_body` too. The API stores them next to the raw text; the Notes page shows the formatted body first with "Show original" for the raw transcript. Edits to the raw text bump the version as today and re-queue enrichment per the existing revision rules.

## Decisions
- Raw first, then format: the Learner can never lose a note to a model failure.
- No review step: the Learner can Undo or edit on the Notes page.
- Formatting is free for the first 5 Voice notes per Learner per local day (owner decision 2026-11-24); after that a Voice note is saved raw and only an explicit Format charges the existing `quick_note` price. No price changes.
- No keys registered here; the hotkey layer binds hold `N` to the exported functions.

## Free formats (owner decision 2026-11-24)
- 5 free formatting turns per Learner per day. The counter is a Postgres row per Learner and local day (`quick_note_format_turns`), reset by the same Learner-local clock as the voice daily cap (`credits.LocalDay`). The API enforces it; the client only displays it.
- A Voice note (anchor `input: voice`) is formatted only while it holds a grant: `quick_notes.format_grant` is `free` (a turn taken at save) or `paid` (an explicit Format request). The grant is set with a single atomic upsert (`used < 5`), so two saves cannot take the same last turn and one note never takes two turns. The grant is spent when the note is formatted and refunded (free turn returned, once) when formatting fails; the raw note is always kept.
- A free turn spends no Credits: the job skips the reservation, uses the bounds of an unreserved `quick_note` quote, and writes the result in one transaction guarded by the claim.
- After the free turns the note is saved `raw_only`, nothing is queued or charged. The Saved toast offers one action, Format; pressing it posts `POST /v1/notes/{id}/format`, which uses a free turn if one is back, else grants `paid` and the existing reservation and settlement charge the unchanged `quick_note` price.
- Editing the raw text never formats again by itself: the old title and body stop being returned (their version no longer matches) and Format is offered again.
- The rail icon shows the free turns left as a number in a small circle (5 to 1), hidden at 0 (`aria-label` "N free formats left"). `GET /v1/notes/format-turns` returns `limit`, `used`, `left`, `resets_at`.
- Analytics (closed enums, no text): `quick_note_format_granted` and `quick_note_format_refunded` with `note_id`, `kind` (`free` or `paid`) and `free_left`.
- Data: `title` and `formatted_body` live on `quick_note_enrichments` (account export, erasure and the anonymiser cover them), the counter table is erased with the Learner and is in the backup fixture.
- Provider cost per `note_enrich` call (route `tencent/hy4-flash`, fallback `z-ai/glm-5.3-flash`, `engine/config/prices.toml`; the `quick_note` bound is 1200 input and 300 output tokens): worst case on the primary 1200 x 35 + 300 x 141 = 84,300 nanos = $0.0000843; on the fallback 1200 x 40 + 300 x 79 = 71,700 nanos = $0.0000717. A typical call (about 450 in, 150 out) costs about 36,900 nanos = $0.000037. Five free turns cost at most $0.00042 per Learner per day.
