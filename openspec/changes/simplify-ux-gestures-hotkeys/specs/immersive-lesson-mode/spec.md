## MODIFIED Requirements

### Requirement: Touch gestures and lock
In immersive mode the player SHALL recognize the gestures defined by `touch-gestures` on the stage: a horizontal swipe as previous or next Slide, a double tap on the left or right half as back or forward 10 seconds on the Lesson timeline (previously previous or next Slide), a long-press as 2x speed while held, a pull down as leaving immersive mode, and a two-finger spread as region selection. A single tap MUST wait for the double-tap interval before toggling the control layer. The control layer MUST offer visible previous Slide, next Slide, back 10 seconds and forward 10 seconds controls as the alternative to every gesture. Touches starting in the screen-edge margins defined by `touch-gestures` MUST be left to the system. A lock control MUST disable all gestures and controls except a "hold to unlock" target. The Slide MUST NOT receive touches.

#### Scenario: Double tap right
- **WHEN** the Learner double-taps the right half of the Slide
- **THEN** the Lesson moves 10 seconds forward and the control layer is not toggled

#### Scenario: Swipe for next Slide
- **WHEN** the Learner swipes left across the Slide
- **THEN** the next Slide is shown

#### Scenario: Locked
- **WHEN** the lock is on and the Learner taps, swipes, holds, pulls or spreads two fingers
- **THEN** nothing changes until the Learner holds the unlock target

### Requirement: Answer panel over the Slide
In landscape the Tutor chat reply SHALL stream into a panel docked to the right edge of the screen, laid over the Slide stage, with a translucent background (about 70 to 80 percent alpha from design tokens, with a blur where supported, a solid-enough fallback where backdrop blur is unsupported, and an opaque background when the Learner prefers reduced transparency). The side rail laid over the stage MUST use the same surface. Text and controls MUST keep a 4.5:1 contrast ratio over both a dark and a white Slide. The selected region MUST stay visible and outlined; when the panel would cover it, the stage MUST shift or scale so the whole region remains visible. The panel MUST be icon-only apart from the Learner's question, the tutor reply, a short follow-up placeholder and the recording timer: it MUST NOT show a title, an auto-send checkbox or explanatory paragraphs. It MUST offer stop while streaming, a follow-up by voice or text and save as Quick note as icon buttons with accessible names. It MUST NOT show a visible close or collapse button: it closes when the Learner swipes it toward the edge it is docked to (mirrored in RTL), presses Escape, taps the Slide outside the panel, or activates a visually hidden, focusable close control. The swipe MAY start only on the panel's leading-edge grab handle or its header, so the reply keeps its vertical scroll and text selection, and a second finger cancels it so the two-finger region gesture keeps priority. The panel follows the finger, closes past 40 percent of its width or on a fast flick, and otherwise springs back; with reduced motion it does not follow. The first time the panel opens on a touch device a toast says "Swipe to close" Explanations, confirmations and errors MUST appear as toasts near the stage edge, not inside the panel. Closing MUST return to the same Slide and time and resume playback only if it was playing before the question. The panel MUST show the same thread as the lesson tutor sheet. In portrait and on screens too narrow for the panel, the lesson tutor sheet MUST be used, with a solid background, with the region shown on a miniature stage. Streaming MUST announce only the start and the finish of a reply to assistive technology. The mascot companion SHOULD show talking while a reply streams.

<!-- Owner (2026-11-23): "duplicate the close button and collapse button in top right corner of the lesson sidebar, just remove both and allow user to swipe left to right from the edge" -->

#### Scenario: Reply shown on the right
- **WHEN** a reply starts streaming in landscape
- **THEN** it appears in a translucent panel on the right and the Slide stays visible through it, while the outlined region remains visible on the left

#### Scenario: Readable over any Slide
- **WHEN** the panel is open over a white Slide and over a dark Slide
- **THEN** its text keeps at least a 4.5:1 contrast ratio on both

#### Scenario: Reduced transparency
- **WHEN** the Learner prefers reduced transparency
- **THEN** the panel and the side rail are opaque

#### Scenario: Region under the panel
- **WHEN** the selected region lies in the right third of the Slide
- **THEN** the stage shifts so the whole region is visible beside the panel

#### Scenario: Swipe to close
- **WHEN** the Learner drags the answer panel from its grab handle past 40 percent of its width toward its docked edge and lifts
- **THEN** the panel closes, the conversation stays in the shared tutor thread, and the side rail ask control opens it again

#### Scenario: No visible close buttons
- **WHEN** the answer panel is open
- **THEN** it shows no close or collapse button, and a keyboard or screen-reader user can still reach a close control

#### Scenario: Saved as a Quick note
- **WHEN** the Learner taps the save icon on a reply
- **THEN** a toast confirms the Quick note with an Undo action and the panel shows no text confirmation

#### Scenario: Close resumes
- **WHEN** the Learner closes the panel after asking while the Slide was playing
- **THEN** the Lesson returns to the same Slide and time and playback resumes
