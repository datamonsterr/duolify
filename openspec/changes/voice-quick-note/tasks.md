## 1. Spec and tests
- [ ] 1.1 Delta specs and `openspec validate voice-quick-note --strict`
- [ ] 1.2 E2E: hold, release, toast, note on the Notes page with its anchor chip; no panel opened; Undo removes it; offline queues; mic denied toast
- [ ] 1.3 Integration (API): anchor persisted; enrichment job carries Slide context; `title` and `formatted_body` stored; failure keeps the raw note; export, erasure and anonymiser coverage
- [ ] 1.4 Unit: `useVoiceQuickNote` (pause and resume only if paused by us, cancel, undo), analytics payloads carry no text
- [ ] 1.5 Engine: `note_enrich` golden cases and deterministic checks (see proposal)
## 2. Implementation
- [ ] 2.1 Icon appended to `paths`; catalogue EN and AR
- [ ] 2.2 `useVoiceQuickNote` extracted from `QuickNotePanel`; exported start and stop functions for hotkeys
- [ ] 2.3 Rail button replaces the panel-opening Note button; player control
- [ ] 2.4 Proto, migration, API, engine
- [ ] 2.5 Notes page: Course > Lesson > Slide, timestamp chip, formatted body with Show original
## 3. Verification
- [ ] 3.1 Screenshots: rail idle, recording, saved toast, Notes entry (390x844 and 844x390)
