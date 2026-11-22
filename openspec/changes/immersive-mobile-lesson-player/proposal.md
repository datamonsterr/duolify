## Why

On a phone the Lesson player is a small portrait frame with buttons around it, and asking the tutor or taking a Quick note means leaving the Slide for a sheet and typing. Learners study on phones in short sessions, often with one hand or while moving, so the Lesson should fill the screen like a video, be controlled by touch, and let the Learner ask or note by speaking about exactly what is on screen.

## What Changes

- **Immersive lesson mode**: a landscape, full-screen presentation of the Slide stage on mobile (also available on desktop), entered from the Lesson player and by rotating the device when the Learner allows it.
- **Tap-to-show controls**: one tap shows a control layer that hides again on its own: play/pause, previous Slide, next Slide, replay this Slide, autoplay on/off, a progress bar segmented by Slide, playback speed, captions, narration, Slide list, Bookmark, exit. Double-tap on the left or right half steps to the previous or next Slide. A lock control prevents accidental touches.
- **Region selection**: a two-finger spread gesture draws a rectangle over the Slide; the Learner can adjust it and ask about that region. A button and a keyboard path select a region without the gesture.
- **Ask during the Lesson by voice or text**: a push-to-talk microphone control (hold to speak, release to send) and a text composer, both available in immersive mode without leaving the Slide. Playback pauses while the Learner asks and resumes when they close the answer.
- **Answer panel over the Slide**: the Tutor chat reply streams into a panel on the right side of the screen with an opaque background, laid over the Slide stage; the selected region stays visible and highlighted.
- **Voice Quick notes**: hold the note control to dictate a Quick note that is anchored to the Lesson, Slide, playback time and, if one is selected, the region.
- **Voice input service**: audio is transcribed through the Engine on the OpenRouter route and is never stored; the transcript becomes the Tutor chat message or Quick note text.
- **Slide regions in the Slide manifest**: generated Lessons declare the regions of each Slide (box, time window, plain text) so the API can ground a region question without reading pixels and without trusting client-supplied Slide text. Lessons without regions fall back to whole-Slide grounding.
- **Aligned behaviour**: screen stays awake while playing, system gestures still exit, position and progress sync exactly as today, offline Lessons keep playing with voice and tutor controls disabled and explained.

No existing requirement is removed. Portrait playback and the existing lesson tutor sheet remain.

## Capabilities

### New Capabilities

- `immersive-lesson-mode`: landscape full-screen Lesson presentation, tap-to-show controls, touch gestures, region selection, and the answer panel laid over the Slide.
- `voice-input`: push-to-talk capture, transcription through the Engine, limits, privacy, permission and failure handling, for Tutor chat questions and Quick notes.

### Modified Capabilities

- `lesson-player`: adds Slide regions to the Slide manifest and their validation; adds the stage contract that lets the player present a Slide full-screen in landscape without changing Slide content.
- `tutor-chat`: adds region-grounded questions asked from a Lesson, with server-side context assembly from the Slide manifest.
- `quick-notes`: adds Quick notes anchored to a Lesson moment and region, including notes created from voice.
- `analytics`: adds the event catalogue for immersive mode, region questions and voice input.
- `ai-reliability`: adds model testing and reliability for transcription, region-grounded tutor replies and region generation.

## Impact

- **Client (`apps/client`)**: new immersive presentation of the existing player; gesture layer above the sandboxed Slide iframe; control layer; region selector; answer panel; push-to-talk recorder; orientation and full-screen handling for Tauri mobile (iOS, Android), mobile web and desktop; microphone permission strings in the Tauri mobile configuration; lazily loaded so the first-load bundle budget holds.
- **API (`services/api`)**: transcription endpoint with size, duration and rate limits; lesson tutor endpoint accepts a region and playback time and assembles region context from the Slide manifest; Quick note creation accepts a Lesson anchor; migrations for anchor columns; export, erasure and backup coverage.
- **Engine (`engine`)**: transcription task on the OpenRouter audio transcription endpoint with a fallback model; tutor prompt builder accepts region context; Lesson generation emits Slide regions and validators check them.
- **Proto (`proto`)**: new `Transcribe` RPC; region context on the tutor chat request; regions on the generated Slide message. API and Engine change together.
- **Infra**: no new service. Request body limit for audio at the proxy. No audio is written to object storage.
- **Pricing and Credits**: transcription has a provider cost. This change proposes no new Learner price: a voice question costs the same Credits as the typed Tutor chat action and a voice Quick note the same as a typed one. Adding the transcription rate to the quote policy needs an owner-approved pricing version.
- **ADRs relied on**: ADR 0009 (narration and captions), ADR 0011 and 0012 (launch defaults), ADR 0015 (all AI through OpenRouter), ADR 0018 (structured outputs), ADR 0019 (live features gated on promotion evidence), ADR 0004 (sessions). A new ADR is needed for voice data handling (audio is processed and discarded, never stored).
