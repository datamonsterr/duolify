# Voice quick note: push to talk, release, saved with its place in the Lesson

## Why
Owner request (2026-11-23), verbatim: "also add the note by voice icon button for super quick taking note experience, no sidebar open just push to talk release and the note is saved with timeline, course and content, format well fit with the context of the course".

Today a voice note needs the rail Note button, which opens the Quick note panel, then a second press on its microphone. The Learner loses the Slide behind a panel. A one-gesture note keeps them in the Lesson.

## What Changes
- One icon-only round **Voice note** button on the immersive side rail and in the non-immersive lesson player controls. It uses the existing `VoiceBubble` (hold to talk, release to save; tap toggles; slide off cancels; ring and timer). No panel, sheet or sidebar opens at any point.
- It replaces the rail Note button that opens the panel. Typed notes stay reachable from the answer panel and the Notes page.
- The Lesson pauses while recording and resumes after, only if the Voice note paused it.
- On release the transcript is saved at once as a raw Quick note, with no review step, and a toast says "Saved" with Undo.
- The note carries: Course id, Lesson id, Slide index and title, playback position (ms) at press time, the selected region if any, and created-at. The existing Quick note anchor already carries Slide, playback time, region and `input: voice`; the Course and Lesson ids already travel with the note. Nothing new is stored for the anchor.
- After the raw save, the existing `note_enrich` job also **formats** the note in the background using Course, Lesson and Slide context: fix dictation errors, use the Course's terms, a short title, bullets, formula or code where the subject calls for it, in the Learner's language, no added facts. The raw transcript stays stored and viewable. A failed formatting leaves the raw note.
- The Notes page shows Course > Lesson > Slide and a timestamp chip that opens the Lesson at that Slide and time.
- One exported action function `startVoiceQuickNote` / `stopVoiceQuickNote` for the hotkey layer (hold `N`). This change registers no keys.

## Capabilities
### Modified Capabilities
- `immersive-lesson-mode`: side rail and lesson player controls.
- `voice-input`: Quick note by voice without a panel.
- `quick-notes`: formatted body and title from enrichment, note provenance display.

## Impact
- Client: rail, player controls, a headless `useVoiceQuickNote` hook, Notes page entry, catalogue EN and AR, one icon appended at the end of `paths` in `features/design/Icon.tsx`.
- API: persist enrichment `title` and `formatted_body` beside the existing summary and tags; pass Slide context to the Engine. One migration numbered above the newest on main (`mise run ci:migration-versions`).
- Proto: `EnrichNoteRequest` gains Course title, Lesson title, Slide index, Slide title and Slide text excerpt; `EnrichNoteResponse` gains `title` and `formatted_body`. Both `services/api` and `engine` change in the same PR.
- Engine: `note_enrich` prompt and checks.

## Credits
Enrichment reserves and settles Credits today (`quick_note` action, see `quick-notes` and `credits`). That rule is unchanged: formatting rides on the same job and the same charge. No price changes. A Learner with no spendable Credits keeps the raw note with status `enrichment_pending`.

## Data backup
`title` and `formatted_body` are columns on the Quick note row: they follow its backup, account export, retention and erasure, and the backup anonymiser MUST blank both. The raw transcript is never overwritten. The migration is covered by `mise run ci:migration-versions` and the backup drill fixture.

## Analytics (PostHog)
New client events: `voice_quick_note_started`, `voice_quick_note_saved`, `voice_quick_note_cancelled`, `voice_quick_note_failed` (reason), `voice_quick_note_undone`. Properties: `surface` (`immersive|player`), `duration_ms`, `has_region`, `language`, `reason` for failures. No transcript, title or body text. Existing `quick_note_created` (`entry_point=slide`), `quick_note_enriched` and `quick_note_enrichment_failed` still fire.

## Model testing and reliability
`note_enrich` keeps its cheap-task route, timeout and fallback chain. Golden cases (task `note_enrich`) gain deterministic checks: `title` not empty and at most 80 characters, `formatted_body` adds no number, name or link absent from the transcript and Slide context, same language as the transcript, formula or code fence only when the transcript mentions one. Fallback: any failure, timeout or failed check keeps the raw note and sets `enrichment_failed` with retry. A change to the Hugging Face golden-set dataset is out of scope here and is reported, not made, while evals are being stabilised.
