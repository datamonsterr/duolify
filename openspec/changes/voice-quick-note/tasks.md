## PR C1: client, instant raw save (no engine, proto or migration)
### 1. Spec and tests
- [x] 1.1 Delta specs and `openspec validate voice-quick-note --strict`
- [x] 1.2 E2E: hold, release, toast, note on the Notes page with its breadcrumb and jump chip; no panel opened; Undo removes it; offline queues; mic denied toast (`e2e/voice-quick-note.spec.ts`)
- [x] 1.4 Unit: `useVoiceQuickNote` (pause and resume only if paused by us, cancel, undo), analytics payloads carry no text
### 2. Implementation
- [x] 2.1 Icon appended to `paths`; catalogue EN and AR
- [x] 2.2 `useVoiceQuickNote` over a shared `saveNote` / `undoSavedNote` (also used by `QuickNotePanel`); exported `startVoiceQuickNote` and `stopVoiceQuickNote` for hotkeys
- [x] 2.3 Immersive rail button replaces the panel-opening Note button
- [ ] 2.3b Non-immersive lesson player control (same hook, `surface: player`; deferred, see C2 follow-ups)
- [x] 2.5a Notes page: Course > Lesson > Slide breadcrumb and a timestamp jump chip (titles resolved client-side from the course; raw text only)
- [x] Analytics: `voice_quick_note_*` events (`trackVoiceQuickNote`), coverage row
### 3. Verification
- [x] 3.1 Screenshots: rail idle, recording, saved toast, Notes entry (390x844 and 844x390, light and dark), attached to the PR for the owner gate

## PR C2: enrichment (blocked on owner decisions: credits for formatting, Hugging Face golden set)
- [ ] 1.3 Integration (API): anchor persisted; enrichment job carries Slide context; `title` and `formatted_body` stored; failure keeps the raw note; export, erasure and anonymiser coverage
- [ ] 1.5 Engine: `note_enrich` golden cases and deterministic checks (see proposal)
- [ ] 2.4 Proto, migration, API, engine
- [ ] 2.5b Notes page: formatted body and title with Show original
