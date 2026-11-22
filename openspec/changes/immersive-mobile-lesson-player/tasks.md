## 1. Spike and decisions

- [x] 1.1 Prove full screen and landscape on each platform (Tauri Android, Tauri iOS, Android Chrome, iOS Safari, desktop) with a throwaway build; record what the Tauri 2 mobile APIs support and choose the per-platform path or the CSS-rotated fallback — written up in `docs/design/immersive-lesson-platforms.md`. Verified by running in Chromium only; Tauri Android/iOS, iOS Safari and real Android Chrome behaviour are documentation-only and still need a real-device pass (checklist in the doc).
- [x] 1.2 Prove microphone capture with `MediaRecorder` in the Tauri mobile webviews and mobile browsers; record containers, permission strings and limits (`docs/design/voice-input-platforms.md`; run only in desktop Chromium 158 with a fake device; iOS, Android, WebKit and permission denial or revocation are documentation-only and the iOS secure-context question for the Tauri scheme is open)
- [x] 1.3 Compare transcription models on the OpenRouter audio transcription endpoint (accuracy, latency, cost for recordings up to 30 seconds) and pick primary and fallback (`evals/spikes/transcription/RESULTS.md`; 16 clips and 248 reference words, one run, synthetic technical clips; real Safari AAC not tested; recommendation needs a larger Golden set)
- [x] 1.4 Write the ADR for voice data handling (audio processed and discarded, never stored; provider data collection denied) (`docs/adr/0021-voice-input-data-handling.md`, Proposed; Mistral's own retention text could not be retrieved, so primary ZDR rests on OpenRouter's flag)
- [x] 1.5 Owner decisions (owner: "You decide all", 2026-11-23): enter on rotation by default = yes on touch devices; auto-send after dictation = yes after a 2-second edit window, default off for Vietnamese; daily cap = 60 per Learner; pricing version with a transcription rate and no new Learner price approved (models: primary `qwen/qwen3-asr-0.6b`, fallback `openai/whisper-large-v3-turbo`, ZDR and data collection denied, 0.0005 USD per request). Spike corrections applied to design.md and the voice-input spec.

## 2. Slide regions in the Slide manifest

- [x] 2.1 e2e: a generated Lesson is published with regions and plays unchanged; a Lesson without regions still plays
- [x] 2.2 integration: publication rejects regions outside the stage, over the limits, with markup or an unknown role; restore drill and backup fixtures include a manifest with regions
- [x] 2.3 unit: region schema, box maths, rectangle-to-region intersection ranked by overlap, time-window filtering
- [x] 2.4 Proto: regions on the generated Slide message; API and Engine updated together
- [x] 2.5 Engine: Lesson prompt and strict schema emit regions; validators; corrective retry message for invalid regions (regions name a layout slot from `engine/config/regions.toml`; the Engine pins each `data-slot` element to its slot with an appended stylesheet, so boxes are true by construction: 0 of 72 rendered regions off by more than 1% of the stage in headless Chromium at 1280x720)
- [x] 2.6 API: store regions in the manifest, validate at publication, expose nothing new to the client
- [x] 2.7 Golden set: region generation cases and thresholds; cost and latency compared with Lesson generation before regions; pin the new dataset revision (`datamonsterr/duolify-golden@1bf626ff`, four region cases; same three lesson cases x5 on hy4-flash before/after: failures 1/15 and 1/15, mean output tokens 1,258 to 1,748 (max 2,453, envelope 14,000), mean latency 10.0 s to 11.7 s, mean cost 0.000205 to 0.000338 USD; emission on)

## 3. Immersive presentation and controls (no AI)

- [x] 3.1 e2e (mocked and full-stack suites, 390 px portrait, 844×390 landscape, 1280 px): enter and leave without losing time; tap shows and hides controls; replay this Slide; previous and next; autoplay on and off; double tap; swipe; lock; exit by back and Escape; screenshots of every state — mocked suite only (`e2e/immersive-lesson.spec.ts`, `e2e/player-framed-stage.spec.ts`); full-stack flow suite is owned elsewhere.
- [x] 3.2 e2e: keyboard-only and screen-reader runs; reduced motion; safe-area insets; controls never auto-hide while focused — screen-reader runs are covered by accessible names, live regions and focus order; no real screen reader was run.
- [x] 3.3 e2e: offline downloaded Lesson plays in immersive mode with ask and microphone disabled and explained — offline playback, gestures and Bookmark verified; ask/microphone controls do not exist yet, so their disabled-and-explained state moves to group 4/6.
- [x] 3.4 unit: presentation state machine, gesture recogniser (tap versus double tap, swipe, two-finger spread, accidental touch), auto-hide timer, stage scaling and rotation maths
- [x] 3.5 Client: immersive presentation of the existing player as a lazy chunk; gesture layer above the Slide iframe; control layer; progress bar segmented by Slide; Slide list; Bookmark; wake lock
- [x] 3.6 Client: per-platform full screen and orientation from the spike; enter on rotation setting
- [x] 3.7 API: per-Learner settings (full screen on rotate, autoplay, auto-send after dictation) with migration, export, erasure and anonymizer classification
- [x] 3.8 Analytics: `immersive_mode_entered`, `immersive_mode_exited`, `immersive_control_used` added to the catalogue and emitted; consent respected
- [x] 3.9 Bundle budget check still passes; first-load bundle unchanged

## 4. Region selection, typed questions and the answer panel

- [ ] 4.1 e2e: two-finger spread selects a region and pauses; handles adjust it; "Select area" and keyboard paths give the same result; accidental touch is discarded
- [ ] 4.2 e2e: ask by text about a region; reply streams in the opaque right-hand panel; region stays visible and the stage shifts when it would be covered; stop, follow-up, save as Quick note, collapse, close resumes playback; portrait falls back to the lesson tutor sheet
- [ ] 4.3 integration: lesson tutor turn with a region assembles context from manifest regions; whole-Slide fallback; out-of-range Slide or rectangle and foreign Lesson are rejected with no Credits, message or Engine call; message stores the anchor
- [ ] 4.4 unit: normalized coordinates from touch points under scale and rotation; stage shift maths; panel sizing
- [ ] 4.5 Proto and Engine: region context on the tutor chat request; prompt builder treats region text as untrusted Slide content
- [ ] 4.6 API: region and playback time on the lesson tutor endpoint; anchor columns on tutor messages with migration, export, erasure and anonymizer classification
- [ ] 4.7 Client: region selector, side rail, composer, answer panel sharing the lesson tutor thread
- [ ] 4.8 Golden set: region-grounded question cases (clear answer, unrelated question, several regions, no regions, injection in region text) and thresholds; pin the dataset revision
- [ ] 4.9 Analytics: `lesson_region_selected`, `lesson_region_question_sent`, `lesson_answer_panel_closed`

## 5. Anchored Quick notes

- [x] 5.1 e2e: typed note on a region saves with its anchor; "show in Lesson" opens the Slide at that time with the region outlined; offline note queues and syncs once (`apps/client/e2e/anchored-notes.spec.ts`, mocked suite at 390, 844×390 and 1280: typed note on a region by the keyboard path, anchor in the POST, Show in Lesson with the region outlined framed and immersive, offline queue syncs once, spread and handle adjustment; screenshots in the scratchpad)
- [x] 5.2 integration: note creation accepts and validates the anchor; a removed Lesson leaves a source-unavailable note; export and erasure (`anchored_quick_notes_integration_test.go`: validation, replay keeps one anchor, foreign Lesson 403, removed Lesson reads source unavailable, note erasure cascades; backup fixture and drill include an anchor row)
- [x] 5.3 unit: anchor serialisation in the offline queue (`anchored_note_queue.test.ts`)
- [x] 5.4 API and client: anchor and source on Quick notes with migration and anonymizer classification (`quick_note_anchors` side table, migration `20261124300000`, anonymizer needs no new rule: all columns are ids, numbers or an `_kind` enum; export and erasure list the table)
- [x] 5.5 Analytics: `lesson_anchored_note_created` (emitted by the API on first save, no text)

## 6. Voice input

- [ ] 6.1 e2e: hold to record, release to transcribe, transcript in the composer, auto-send after the edit window, edit cancels auto-send; cancel by sliding off; 30-second stop; dictated Quick note with undo; permission denied; offline; daily cap; voice unavailable without a transcription rate
- [ ] 6.2 integration: transcription endpoint limits (duration, size, container, entitlement, rate limit, daily cap) reject before any Engine call; no audio is written anywhere (assert on storage, logs and queues); settlement of transcription usage against the quote policy; a transcript that is never sent spends no Credits
- [ ] 6.3 unit: recorder state machine, duration and size guards, container selection
- [ ] 6.4 Proto and Engine: `Transcribe` RPC on the OpenRouter audio transcription endpoint with primary and fallback, timeout, data collection denied, typed errors; `transcribe` task in `models.toml`
- [ ] 6.5 API: `POST /v1/voice/transcriptions`; proxy body limit; limit counters in Redis with expiry
- [ ] 6.6 Client: push-to-talk control, level meter and timer, tap-to-toggle, permission flow; Tauri mobile microphone permission strings
- [ ] 6.7 Golden set: transcription cases (languages, accents, noise, silence, technical terms) with word-error-rate, latency and cost thresholds; promotion evidence for the task; pin the dataset revision
- [ ] 6.8 Analytics: `voice_recording_finished`, `voice_recording_cancelled`, `voice_transcription_succeeded`, `voice_transcription_failed`, `voice_transcript_used`, `voice_transcript_discarded`; `$ai_generation` mapping for transcription without content

## 7. Documentation, backup and release

- [ ] 7.1 Update `docs/data-inventory.md` (anchors, settings, no audio retention) and the backup notes; restore drill covers the new columns
- [ ] 7.2 Update `.env.example` and the deploy compose with the new limits and feature switches
- [ ] 7.3 Design review against `docs/design` at phone and desktop sizes with screenshots attached to the PR
- [ ] 7.4 Full-stack flow: a Learner plays a Lesson in immersive mode, selects a region, asks by text, saves the reply as a Quick note; screenshots for owner review
- [ ] 7.5 Sync the delta specs into the main specs when implementation is merged
