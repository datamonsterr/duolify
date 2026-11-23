# immersive-lesson-mode Specification

## Purpose
TBD - created by archiving change immersive-mobile-lesson-player. Update Purpose after archive.
## Requirements
### Requirement: Landscape full-screen presentation
The Lesson player SHALL offer an immersive presentation in which the Slide stage fills the screen in landscape. Entering or leaving it MUST NOT reload the Slide, reset the playback clock, change the synced position, or stop Narration. The stage MUST keep its 16:9 coordinate space and be letterboxed on other aspect ratios; controls MUST respect safe-area insets and Slide content MUST NOT be cropped. Where the platform cannot lock orientation, the player MUST present the stage rotated so that it is landscape relative to the long edge of the screen. The Learner MUST be able to leave with a visible exit control, the system back gesture and the Escape key.

#### Scenario: Enter while playing
- **WHEN** the Learner taps the expand control while a Slide is playing at 12 seconds
- **THEN** the stage fills the screen in landscape and playback continues from 12 seconds with the same Narration and captions state

#### Scenario: Platform cannot lock orientation
- **WHEN** the Learner enters immersive mode on a platform without orientation lock while holding the device in portrait
- **THEN** the stage is shown rotated to landscape and follows the device when it is turned

#### Scenario: Leave returns to the framed player
- **WHEN** the Learner uses the system back gesture in immersive mode
- **THEN** the framed player is shown on the same Slide at the same time and the app does not navigate away from the Lesson

### Requirement: Enter on rotation is a Learner choice
The player SHALL enter immersive mode when the device is turned to landscape during a Lesson only while the Learner's "full screen on rotate" setting is on and the system rotation lock allows it. The setting MUST be on by default on mobile, changeable in settings and in the control layer, and stored per Learner.

#### Scenario: Setting off
- **WHEN** the setting is off and the Learner turns the device to landscape
- **THEN** the framed player stays and only the expand control enters immersive mode

### Requirement: Tap-to-show control layer
In immersive mode one tap on the Slide SHALL show a control layer and a second tap SHALL hide it. The layer MUST offer: play or pause, previous Slide, next Slide, replay this Slide, autoplay on or off, a progress bar segmented by Slide with elapsed and total time, playback speed, captions, Narration, the Slide list, Bookmark, and exit. The layer MUST hide by itself after 3 seconds without interaction, and MUST NOT hide while focus is inside it, while assistive technology is active, or while playback is stopped at the end of a Slide. Every control MUST have an accessible name, be reachable by keyboard and switch access, and be at least 44 by 44 CSS pixels.

#### Scenario: Show, use and auto-hide
- **WHEN** the Learner taps the Slide, taps "replay this Slide" and then does nothing for 3 seconds
- **THEN** the Slide and its Narration restart from zero and the control layer hides

#### Scenario: End of Slide with autoplay off
- **WHEN** a Slide ends and autoplay is off
- **THEN** the control layer stays visible with next Slide and replay this Slide offered, and playback does not advance

#### Scenario: Autoplay on
- **WHEN** a Slide ends and autoplay is on
- **THEN** a cancellable countdown is shown and the next Slide starts when it finishes

### Requirement: Touch gestures and lock
In immersive mode the player SHALL recognize: a double tap on the left or right half as previous or next Slide, a horizontal swipe as previous or next Slide, and a two-finger spread as region selection. A single tap MUST wait for the double-tap interval before toggling the control layer. A lock control MUST disable all gestures and controls except a "hold to unlock" target. The Slide MUST NOT receive touches.

#### Scenario: Double tap right
- **WHEN** the Learner double-taps the right half of the Slide
- **THEN** the next Slide is shown and the control layer is not toggled

#### Scenario: Locked
- **WHEN** the lock is on and the Learner taps, swipes or spreads two fingers
- **THEN** nothing changes until the Learner holds the unlock target

### Requirement: Region selection
The Learner SHALL be able to select one rectangular region of the Slide. A two-finger spread MUST start a selection whose opposite corners follow the two touch points and which becomes fixed when both fingers lift; playback MUST pause when the gesture starts. A fixed region MUST show handles for adjustment and the ask and note controls. A region smaller than 6% of the stage in width or height MUST be discarded. The same result MUST be reachable without the gesture: a "Select area" control that places a rectangle to move and resize with one pointer, and a keyboard mode. The region MUST be expressed in normalized stage coordinates together with the playback time.

#### Scenario: Select with two fingers
- **WHEN** the Learner places two fingers on a diagram and spreads them
- **THEN** playback pauses, a rectangle follows the fingers, and on release it is fixed with handles and the ask and note controls beside it

#### Scenario: Accidental touch
- **WHEN** two fingers touch and lift almost in place
- **THEN** no region is created and playback resumes if it was playing

#### Scenario: Select with the keyboard
- **WHEN** the Learner activates "Select area", moves and resizes the rectangle with the keyboard and confirms
- **THEN** the same fixed region with ask and note controls is shown

### Requirement: Ask and note without leaving the Slide
Immersive mode SHALL keep a side rail with ask (microphone and text), note, and select-area controls reachable even while the control layer is hidden. Opening the ask or note composer MUST pause playback. Sending a question MUST use the lesson Tutor chat with the current Lesson, Slide, playback time and the selected region if there is one. With no region selected the question MUST apply to the current Slide.

#### Scenario: Ask by text about a region
- **WHEN** the Learner selects a region, types a question and sends it
- **THEN** the question is sent with that region and time, and the reply opens in the answer panel

#### Scenario: Ask with no region
- **WHEN** the Learner sends a question without selecting a region
- **THEN** the question is about the current Slide and the reply opens in the answer panel

### Requirement: Answer panel over the Slide
In landscape the Tutor chat reply SHALL stream into a panel docked to the right edge of the screen, laid over the Slide stage, with an opaque background. The selected region MUST stay visible and outlined; when the panel would cover it, the stage MUST shift or scale so the whole region remains visible. The panel MUST offer stop while streaming, a follow-up by voice or text, save as Quick note, collapse, and close. Closing MUST return to the same Slide and time and resume playback only if it was playing before the question. The panel MUST show the same thread as the lesson tutor sheet. In portrait and on screens too narrow for the panel, the lesson tutor sheet MUST be used with the region shown on a miniature stage. Streaming MUST announce only the start and the finish of a reply to assistive technology.

#### Scenario: Reply shown on the right
- **WHEN** a reply starts streaming in landscape
- **THEN** it appears in an opaque panel on the right while the Slide and the outlined region remain visible on the left

#### Scenario: Region under the panel
- **WHEN** the selected region lies in the right third of the Slide
- **THEN** the stage shifts so the whole region is visible beside the panel

#### Scenario: Close resumes
- **WHEN** the Learner closes the panel after asking while the Slide was playing
- **THEN** the region is cleared and playback resumes from the time it paused

### Requirement: Playback comfort and safety in immersive mode
While a Slide is playing in immersive mode the screen SHALL stay awake, and the wake lock MUST be released when playback pauses, the app goes to the background or immersive mode ends. Position, progress, completion, Bookmarks and offline behaviour MUST be identical to the framed player. Offline, the ask and microphone controls MUST be disabled with an explanation while playback, gestures, Bookmarks and typed Quick notes keep working. Reduced-motion preference MUST remove the stage shift animation and the autoplay countdown animation.

#### Scenario: Screen stays awake
- **WHEN** a Slide plays for longer than the device's sleep timeout
- **THEN** the screen stays on, and the device may sleep again after the Learner pauses

#### Scenario: Offline
- **WHEN** the Learner plays a downloaded Lesson offline in immersive mode
- **THEN** playback and gestures work and the ask and microphone controls explain that they need a connection

### Requirement: Immersive mode analytics events
The client SHALL emit content-free events from the analytics catalogue for immersive mode: entering and leaving, control use, gestures, region selection, and answer panel actions. Events MUST carry stable identifiers and closed enums only, MUST NOT carry question text, transcript text, note text or Slide text, and MUST respect the Learner's analytics consent.

#### Scenario: Region question funnel
- **WHEN** a Learner enters immersive mode, selects a region, asks by text and closes the panel
- **THEN** `immersive_mode_entered`, `lesson_region_selected`, `lesson_region_question_sent` and `lesson_answer_panel_closed` are emitted with Course, Lesson and Slide identifiers and no free text

### Requirement: Immersive mode data backup and retention
Immersive mode SHALL create no data of its own beyond per-Learner settings (full screen on rotate, autoplay, auto-send after dictation), which MUST be stored with the Learner profile, included in account export, removed on account erasure, and covered by the Postgres backup and restore drill. Region rectangles and playback times MUST be stored only as anchors on the Tutor chat messages and Quick notes they belong to and follow those records' retention.

#### Scenario: Erasure
- **WHEN** a Learner's account is erased
- **THEN** their immersive settings and every stored region anchor are removed with the profile, messages and notes they belong to

### Requirement: Immersive mode model testing and reliability
Questions asked from immersive mode SHALL use the Tutor chat task and its reliability rules. The Golden set MUST contain region-grounded lesson questions, and the feature MUST stay closed on a live Engine without promotion evidence for that task. When the tutor is unavailable, the answer panel MUST show the retryable unavailable state, keep the Learner's draft, and spend no Credits.

#### Scenario: Tutor not promoted
- **WHEN** the live Engine has no promotion evidence for region-grounded tutor replies
- **THEN** the ask control explains that answers are unavailable, the draft is kept and no Credits are reserved

