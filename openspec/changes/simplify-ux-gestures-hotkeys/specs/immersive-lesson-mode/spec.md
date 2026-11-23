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
