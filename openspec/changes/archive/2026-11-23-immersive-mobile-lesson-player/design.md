## Context

Lessons are Motion HTML Slides played live, one Slide at a time, inside `<iframe sandbox="allow-scripts">` without `allow-same-origin`. The host drives a Slide through a `postMessage` protocol (`play`, `pause`, `seek`, `rate`, `ended`, `checkpoint`) and owns the clock. The host cannot read the Slide's DOM or pixels. Narration, captions, position sync, offline playback and the lesson tutor sheet already exist. The lesson tutor sheet sends only a Lesson id and Slide index; the API assembles context.

The client is Tauri 2 with React. Mobile builds (iOS, Android) are planned; the same code also runs as a mobile web page. The Engine reaches models only through OpenRouter (ADR 0015), and live AI features run only with promotion evidence (ADR 0019).

The owner asked for: landscape full-screen Slides on mobile; one tap to show controls (navigate, autoplay, next, previous, replay this Slide); notes and questions by voice with push-to-talk; a two-finger spread to select a region and ask about it by voice or text; the answer shown on the right of the screen over the Slide with an opaque background; and other video-style learning features in the same spirit.

## Goals / Non-Goals

**Goals:**

- The Slide fills a landscape phone screen and is controlled like a video.
- A Learner can ask about a chosen part of a Slide, or take a note, without leaving the Slide and without typing.
- Region questions are grounded on content the server authorizes, not on text the client claims is on screen.
- Every gesture has a visible, keyboard- and screen-reader-reachable alternative.
- Nothing new is stored about a Learner's voice.

**Non-Goals:**

- Rendering Lessons to video files.
- Pinch-to-zoom of the Slide (the two-finger gesture is reserved for region selection).
- Always-on listening, wake words, or a spoken AI voice conversation. Replies are text; reading a reply aloud is a later change.
- Image understanding of Slide pixels.
- A new price for voice. Pricing changes go through the owner approval flow.
- Redesigning portrait playback or the desktop shell.

## Decisions

### 1. Immersive mode is a presentation of the existing player, not a second player

One player state machine (clock, position sync, narration, captions, offline) with two presentations: the current framed one and the immersive one. Entering or leaving immersive mode never reloads the Slide, never resets the clock and never changes what is synced.

*Alternative considered:* a separate full-screen player route. Rejected: duplicates playback, sync and offline logic, and loses position on switch.

### 2. Full screen and landscape, per platform

| Platform | Full screen | Landscape |
|---|---|---|
| Tauri Android | system bars hidden through the window API | orientation locked through a small mobile plugin command |
| Tauri iOS | status bar and home indicator hidden through a plugin command | supported-orientations request through the same plugin |
| Mobile web, Android Chrome | Fullscreen API on the player container | `screen.orientation.lock('landscape')` inside full screen |
| Mobile web, iOS Safari | no element full screen on iPhone | not lockable: the stage is rotated 90° with CSS when the device is portrait, and follows the device when it is turned |
| Desktop | Fullscreen API or window full screen | not applicable |

The first implementation task is a spike that proves each row on a real device or emulator and records what the Tauri 2 mobile APIs support at that time. If orientation lock is unavailable on a platform, the CSS-rotated stage is the fallback everywhere.

Entry: an "expand" control in the player; optionally rotating the device to landscape (a setting, on by default on mobile, respecting the system rotation lock). Exit: the exit control, the system back gesture, Escape, or rotating back when entry was by rotation.

The Slide stage keeps its 16:9 coordinate space and is letterboxed on other aspect ratios. Safe-area insets apply to controls, never to Slide content.

### 3. A host gesture layer above the iframe

A transparent layer owned by the host sits over the Slide iframe in immersive mode and receives all touches. Slides are not interactive during playback today, so nothing is lost. The layer recognizes:

- single tap: show or hide controls;
- double tap on the left or right half: previous or next Slide (tap-to-show waits the double-tap interval before acting);
- horizontal swipe: previous or next Slide;
- two-finger spread: region selection (decision 5);
- long press: nothing by default (reserved).

Controls auto-hide after 3 seconds of no interaction, never while focus is inside them, never while a screen reader is active, and never while paused at the end of a Slide. A lock control disables the gesture layer except for a "hold to unlock" target.

### 4. Controls shown on tap

Centre: previous Slide, play/pause, next Slide. Bottom: a progress bar segmented by Slide with elapsed and total time, replay this Slide, autoplay toggle, speed, captions, narration, Slide list. Top: Lesson title, Bookmark, exit. Side rail, always reachable by a small handle even when other controls are hidden: ask (microphone and text), note, select region.

Autoplay means: when a Slide ends, advance to the next after a short countdown that can be cancelled; off means stop at the end of each Slide. This is the existing auto-advance setting surfaced in the control layer, stored per Learner as today.

### 5. Region selection

Two fingers down on the Slide start a selection: the two touch points are opposite corners of a rectangle that follows the fingers; lifting both fingers fixes it. Playback pauses when the gesture starts. The fixed region shows corner handles for adjustment, a dimmed surround, and the ask and note controls beside it. A region smaller than 6% of the stage in either dimension is treated as an accidental touch and discarded. One region at a time.

Alternatives for people who cannot or do not want to use the gesture: a "Select area" control that places a default rectangle to drag and resize with one finger, mouse or pen, and a keyboard mode (arrow keys move, Shift+arrow resizes, Enter confirms).

The region is expressed in normalized stage coordinates (`x`, `y`, `w`, `h` in 0..1 of the 16:9 stage) plus the playback time in milliseconds.

*Alternative considered:* long-press and drag with one finger. Kept as the accessible alternative rather than the default because the owner asked for the two-finger spread and because long-press conflicts with text selection habits.

### 6. Grounding a region without pixels: Slide regions in the Slide manifest

The host cannot read the sandboxed Slide, and the server cannot lay out HTML. So the content of a region must be declared, not observed.

Lesson generation emits, for each Slide, a list of **regions**: `id`, box in the same normalized coordinates, optional time window on the Slide clock, a short plain-text description of what the region shows, and its role (title, explanation, example, diagram, code, question, caption). The publisher validates them (inside the stage, bounded count, bounded text length, no markup, no overlap above a threshold between regions of the same time window) and stores them in the Slide manifest. The API resolves a Learner's rectangle and playback time to the declared regions it intersects, ranked by overlap, and passes their text to the Engine together with the Lesson context it already assembles.

The client sends only Lesson id, Slide index, rectangle and time. It never sends Slide text. This keeps the existing rule that tutor context is server-assembled and authorized.

Lessons published before this change, and authored example Courses until they are re-authored, have no regions: the API then grounds on the whole Slide (using the existing manifest explanation) and tells the Engine which part of the stage was selected in words ("upper left", "centre"), and the reply says it is answering about the Slide as a whole when it cannot tell what the region contains.

*Alternatives considered:*
- A read-only `region_text` message answered by a runtime shim inside the Slide. Rejected as the primary source: the Slide is untrusted generated HTML, so its answer is client-supplied text the server cannot verify; it would also need a trusted shim inside every Slide.
- Capturing a screenshot of the region and using a vision model. Rejected: the host cannot capture a cross-origin sandboxed iframe on the web, native capture differs per platform, and it adds a vision route and cost.
- Headless rendering on the server to compute boxes. Rejected: heavy for a 4 GB VM and still needs semantic text.

### 7. The answer panel

In landscape the reply streams into a panel docked to the right edge, about 40% of the width (minimum 320 px, maximum 480 px), full height inside the safe area, with an opaque surface colour from the design tokens (not translucent, so text contrast never depends on the Slide behind it). The Slide stage stays where it is and the panel covers its right part. If the selected region would be hidden by the panel, the stage shifts left just enough to keep the whole region visible; if it cannot fit, the region is scaled into the visible area. The region stays outlined.

The panel shows: the Learner's question (or transcript), the streamed reply, Stop while streaming, then follow-up (microphone and text), save as Quick note, and close. Closing returns to the Slide at the same time and resumes playback if it was playing before the question. The thread is the same lesson tutor thread the existing sheet uses, so questions asked in either presentation appear in both.

In portrait and on narrow screens the existing lesson tutor sheet is used, with the region shown as a thumbnail outline on a miniature stage.

### 8. Push-to-talk

Hold the microphone control to record, release to finish. A visible level meter and timer show while recording; sliding the finger off the control or pressing Escape cancels. Maximum 30 seconds, minimum 0.5 seconds. On release the audio is uploaded, the transcript appears in the composer, and:

- for a question it is sent automatically after a 2-second window in which the Learner can edit or cancel (a setting can require an explicit send). The setting defaults on, except that it defaults off when the transcript language is Vietnamese until the Golden set measures Vietnamese word error rate;
- for a Quick note it is saved immediately as a raw Quick note, with undo.

A tap (instead of a hold) on the microphone toggles recording on and off, for people who cannot hold. Typed input is always available next to the microphone.

Recording uses `MediaRecorder`. The client probes `audio/webm;codecs=opus` and then `audio/mp4`, uploads the Blob with `recorder.mimeType`, and lets the API identify the container itself (a browser may report `audio/mp4` yet hold Opus, and Safari 18.4 and later also records WebM/Opus). It requests `audioBitsPerSecond: 32000` (30 s is about 113 KB) and checks the Blob size, because Safari's bitrate options are not documented as supported. A level gate keeps near-silent recordings from being uploaded.

### 9. Transcription path and privacy

`POST /v1/voice/transcriptions` (authenticated, multipart audio, a `purpose` of `tutor` or `note`, and the Lesson id) → the API checks limits, forwards the bytes to the Engine's new `Transcribe` RPC → the Engine calls the OpenRouter audio transcription endpoint with data collection denied → text, detected language and duration come back.

Audio is held in memory only, is never written to disk, object storage, logs, analytics or backups, and is dropped when the request ends. Only the transcript persists, as the Tutor chat message or Quick note it becomes, under the existing retention and erasure rules for those. A failed or empty transcription never produces a message or a note.

Limits: 30 seconds, 1 MB, per-Learner rate limit and a daily cap of 60 transcriptions per Learner (configurable), Lesson must be one the Learner is entitled to.

Models (decided from the spike): primary `qwen/qwen3-asr-0.6b`, fallback `openai/whisper-large-v3-turbo`, both called with `provider.zdr = true` and `data_collection = "deny"`, and a per-request cost ceiling of 0.0005 USD. They go in `engine/config/models.toml` as a `transcribe` task and still need promotion evidence from the Golden set (ADR 0019). Whisper-class models hallucinate text on silence, so the client's level gate runs before upload and the Golden set keeps the empty-text case.

Endpoint contract (as built): `POST /v1/voice/transcriptions`, `multipart/form-data` with `audio` (the file, at most 1,000,000 bytes), `purpose` (`tutor` or `note`), `lesson_id` (a Lesson of the signed-in Learner) and `duration_ms` (client measured, 500 to 30000). `200` returns `{text, language, duration_ms, served_model, fallback_used}`; an empty `text` means nothing was understood and creates nothing. Errors are `{"error": code}`: `voice_unavailable` 503 (no approved transcription rate, a rate that charges Credits, Redis or the Engine's promotion evidence unavailable), `voice_too_long` 400, `voice_too_short` 400, `voice_invalid` 400, `voice_too_large` 413, `voice_unsupported_container` 415, `voice_not_entitled` 403, `voice_rate_limited` 429 with `Retry-After` (6 per minute per Learner), `voice_daily_cap` 429 with `resets_at` (RFC 3339, next UTC midnight; 60 per day, from the pricing version's `transcribe.daily_requests`) and `voice_failed` 502 (retryable). The container is identified from the bytes (EBML `webm` doctype or an ISO `ftyp` brand) and the length is measured from the container where it can be (WebM clusters or Info Duration, MP4 `mvhd`); a recording longer than it was declared is refused. The multipart body is streamed (`MultipartReader`, never `ParseMultipartForm`), so nothing reaches a temp file. Caddy refuses a body over 1100 KB on this route before the API. Counters live in Redis, always with an expiry, and a failed request gives its daily slot back. The Engine `Transcribe` RPC fails with an `ErrorInfo` in domain `engine.duolify.transcribe` (`invalid_audio`, `transcribe_failed`, `transcribe_timeout`, `transcribe_cost_ceiling`), kept apart from the promotion denial domain `engine.duolify`. Settlement: the answer must stay inside the active version's `transcribe.max_cost_nanos` and the 30 second limit, else it is not delivered and the API logs `voice_over_policy`; transcription costs no Credits, so a transcript that is never sent spends nothing.

Privacy wording: "audio is never stored" holds for Duolify systems only. Learner-facing text and the spec say the audio is "processed by a speech provider that does not keep it", with zero data retention as the mechanism.

### 10. Credits and pricing

A voice question is a Tutor chat turn and costs what that action costs today, with the same quote, daily free allowance and zero-Credits behaviour. A voice Quick note costs what a Quick note costs today. Transcription itself has no separate Learner price.

The owner approved a pricing version that adds a transcription rate and no new Learner price (it is prepared on `feat/pricing-voice-preview`). The daily cap of 60 and the 30-second limit bound the provider cost; transcription is free to the Learner.

Transcription still has a provider cost that settlement must account for. That needs a transcription rate in the quote policy of an owner-approved pricing version. Until that version is active, voice input is unavailable and the microphone control explains that typing still works. Nothing in this change approves pricing.

### 11. Offline and degraded states

Offline, immersive playback, controls, gestures, Bookmarks and typed Quick notes (offline queue) work as today. The microphone and ask controls are disabled with a short explanation; a region can still be selected and attached to a typed Quick note. With microphone permission denied, the control explains how to enable it and typing remains. If the transcription or tutor feature is not promoted on a live Engine (ADR 0019), the control shows the same retryable "unavailable" state the app already uses.

### 12. Accessibility

Every gesture has a control (decisions 3 and 5). Controls are reachable in a logical order by keyboard and switch access, have accessible names, and meet target size. The answer panel is a labelled region; streaming announces only start and finish, as the existing sheet does. Captions remain available in immersive mode and never sit under the panel. Reduced motion removes the stage shift animation and the countdown ring. Voice is never the only way to do anything.

### 13. Data model

- Tutor messages gain an optional anchor: Slide index, playback time, region rectangle, and the ids of the declared regions that grounded the reply.
- Quick notes gain the same optional anchor and a `source` of `typed` or `voice`.
- The Slide manifest gains `regions` per Slide.
- A per-Learner setting stores: enter immersive mode on rotation, autoplay, auto-send after dictation.

No audio column, table or object exists.

## Risks / Trade-offs

- **Orientation lock and full screen on iOS are limited** → a CSS-rotated stage is the universal fallback; the spike decides the per-platform path before the UI is built.
- **Two-finger spread is the system zoom gesture in browsers** → the stage sets `touch-action: none` in immersive mode and offers the "Select area" control; page zoom remains available outside immersive mode.
- **Generated regions can be wrong or missing** → validators reject impossible regions, the API falls back to whole-Slide grounding, golden cases measure region-grounded answers, and the reply states when it cannot tell what the region contains.
- **Region generation adds tokens to every Lesson** → bounded count and text length per Slide; cost and latency compared against today's Lesson generation in the eval before promotion.
- **Transcription errors change the question** → the transcript is shown and editable before it is sent; language is detected, and low-confidence or empty results ask the Learner to retry or type.
- **Microphone use is sensitive** → recording only while the control is held or toggled, a visible indicator the whole time, no storage of audio, a new ADR recording this, and the provider called with data collection denied.
- **A new provider cost without a new price** → per-Learner daily cap and the 30-second limit bound it; the owner can price it later through the approval flow.
- **Overlay hides part of the Slide** → the stage shifts to keep the selected region visible; the panel can be collapsed to a handle.
- **More client code on a constrained first load** → immersive mode, recorder and panel are lazy chunks covered by the bundle budget check.

## Migration Plan

1. Land the Slide manifest `regions` field and validators with generation behind the eval; existing Lessons stay valid without it.
2. Land immersive presentation, controls and gestures (no AI).
3. Land region selection with typed questions and anchored typed Quick notes.
4. Land voice input after the pricing version with the transcription rate is approved and the transcription task has promotion evidence.

Each step is shippable on its own. Rollback is disabling the immersive entry control or the microphone control by configuration; no data migration is needed to roll back.

## Open Questions

- Resolved 2026-11-23 (owner: "You decide all"): rotating the device enters immersive mode by default on touch devices (`fullscreen_on_rotate` defaults true, already shipped); a voice question is sent automatically after a 2-second edit window, default off when the transcript language is Vietnamese; the daily cap is 60 transcriptions per Learner; the pricing version with a transcription rate and no new Learner price is approved.
- Should the reply be read aloud with the Lesson's narration voice? Out of scope here; it would spend narration characters.
- Should the two authored example Courses be re-authored with regions? Proposed: yes, in the change that builds them or a follow-up.

## Spike findings (2026-11-22, tasks 1.2 to 1.4)

Proposed corrections from `docs/design/voice-input-platforms.md`, `evals/spikes/transcription/RESULTS.md` and ADR 0021. Applied on 2026-11-23 after the owner's "You decide all", with the owner's model choice (primary `qwen/qwen3-asr-0.6b`, fallback `openai/whisper-large-v3-turbo`) instead of Voxtral: the container probe, 32 kbps request, level gate and privacy wording are in decisions 8 and 9 and in the `voice-input` spec; the per-request ceiling is 0.0005 USD, not the 0.005 USD proposed in the spike.

- **Decision 8, containers.** "WebM/Opus on Android and desktop Chromium, MP4/AAC on iOS" is not a safe rule: Chromium reports `audio/mp4` as supported but it holds Opus, and Safari 18.4 and later also records WebM/Opus. Correct to: probe `audio/webm;codecs=opus` then `audio/mp4`, upload `recorder.mimeType`, and let the API identify the container itself.
- **Decision 8 and 9, size limit.** Chromium's default bitrate is about 129 kbps (30 s = 483 KB, within 2x of the 1 MB limit). Add: the client requests `audioBitsPerSecond: 32000` (30 s = about 113 KB) and checks the Blob size, because Safari's bitrate options are not documented as supported.
- **Decision 9, model choice and "provider data collection denied".** Denying data collection is routable for every candidate, but zero data retention is not: OpenRouter has no ZDR endpoint for `openai/gpt-4o-mini-transcribe`, `openai/gpt-4o-transcribe` or `google/gemini-3.5-transcribe`. Replace the example candidates (`gpt-4o-mini-transcribe`, `whisper-large-v3-turbo`) with the recommendation: primary `mistralai/voxtral-mini-transcribe-3-0`, fallback `qwen/qwen3-asr-0.6b`, and require `provider.zdr = true` as well as `data_collection = "deny"`. `gpt-4o-mini-transcribe` also had 13.1% WER on the Vietnamese clips.
- **Decision 9 and spec "Silence".** The silence rule fails for Whisper and `gpt-4o-transcribe` ("Thank you.", "Blessing", "こんにちは。"). Add a level gate so near-silent recordings are not uploaded, and keep the "empty text" requirement for the Golden set.
- **Decision 9 and the privacy claim.** "Audio is never stored" holds for Duolify systems only; the provider is a separate party. Word the Learner-facing text and the spec's privacy requirement as "processed by a speech provider that does not keep it", and name ZDR as the mechanism.
- **Decision 9, limits and Decision 10.** At the proposed daily cap of 60, the worst case on the primary is about 0.10 USD per Learner per day (about 0.0017 USD per 30 s). Typical 5 s dictation costs about 0.0003 USD. Fits "no new Learner price", but the transcription rate in the pricing version must use the primary's rate and a per-request ceiling of 0.005 USD.
- **Decision 8, auto-send.** Not contradicted. Observed Vietnamese WER of 3% to 9% on a few clips supports keeping the 2-second edit window; applied: auto-send defaults off when the transcript language is Vietnamese.
- **Impact, client.** iOS needs `NSMicrophoneUsageDescription` (use `src-tauri/Info.ios.plist`), Android needs `RECORD_AUDIO` and `MODIFY_AUDIO_SETTINGS` in the generated manifest; no Tauri plugin or capability is required. Whether `getUserMedia` is available under the iOS `tauri://` scheme is unverified and must be checked on a device before 6.6.
