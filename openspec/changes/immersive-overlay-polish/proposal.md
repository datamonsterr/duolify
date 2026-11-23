# Immersive overlay polish: see-through panel, bubble buttons, icon-only sidebar

## Why
Owner requests (2026-11-23), verbatim:
1. "The sidebar on mobile when learning need to be see through opacity in the background"
2. "Note to talk is just a small bubble button"
3. "Reduce explain text use more icon and remove the title, checkbox and explaination, explain using the bubble toast of phone only not in the sideba."

The opaque answer panel hides the Slide it is about, the Talk and Note controls are wide text buttons, and the panel carries a title, an auto-send checkbox and helper paragraphs.

## What Changes
- The right-docked answer panel and the side rail over the Slide stage use a translucent token surface with a blur, a solid fallback where unsupported and opaque under `prefers-reduced-transparency`. Text keeps 4.5:1 contrast over a dark and a white Slide. The portrait tutor sheet stays solid.
- Talk (push-to-talk) and Note become small round floating icon bubbles with the recording state on the bubble.
- The panel loses its title, the auto-send checkbox (moves to player settings) and helper text; its text buttons become icon buttons.
- A small shared toast (`apps/client/src/components/toast/`) carries every explanation: microphone permission, offline, daily cap, voice unavailable, recording too short or cancelled, nothing understood, saved as Quick note (Undo), errors.
- The mascot talks while a tutor reply streams in immersive mode.

## Capabilities
### Modified Capabilities
- `immersive-lesson-mode`: "Answer panel over the Slide" (translucent, icon-only, toasts).
- `voice-input`: "Microphone permission and unavailable states" and "Transcript review before use" (toasts, setting in player settings).

## Impact
Client only. No API, proto, engine or database change; no AI behaviour change.

## Data backup
No new data. `auto_send_after_dictation` already lives with the Learner profile, in the account export and the backup drill.

## Analytics (PostHog)
No new events. Existing voice and immersive events keep their names and properties. The Learner-visible toast is not an analytics event here; the app-wide `toast_shown` event belongs to `simplify-ux-gestures-hotkeys`.

## Model testing and reliability
No model, prompt or fallback changes. Voice failure classes still map to the same states; only the surface that shows them changes. Unit and e2e tests assert each state still reaches the Learner.
