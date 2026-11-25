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
- Formatting adds no charge and no price; it shares the existing `quick_note` reservation.
- No keys registered here; the hotkey layer binds hold `N` to the exported functions.
