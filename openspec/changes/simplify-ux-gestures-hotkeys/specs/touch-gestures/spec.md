## ADDED Requirements

### Requirement: Gesture vocabulary
On phones and tablets in touch mode the client SHALL support exactly these gestures, each with the one meaning below, implemented by one shared gesture layer (pointer events, `touch-action` set per surface, transform-only animation) and not by per-screen code: swipe toward the edge to close a sidebar, side panel or the answer panel; swipe down to dismiss a bottom sheet and a toast; horizontal swipe on the lesson stage for previous or next Slide; double tap on the left or right half of the lesson stage to seek back or forward; single tap on the stage to show or hide controls; long-press on the stage for 2x speed while held; pull down on the stage to leave immersive mode; two-finger spread on the stage to select a region (unchanged). No other gesture is added. Horizontal swipe between bottom tabs is NOT used.

#### Scenario: One layer
- **WHEN** a new surface needs swipe-to-close
- **THEN** it uses the shared layer with a direction and a target element and adds no gesture code of its own

### Requirement: Swipe to close panels and sheets
A sidebar, side panel or answer panel SHALL close when swiped toward the edge it entered from, and a bottom sheet SHALL close when swiped down. The panel MUST follow the finger one to one along its axis and resist the opposite direction by a factor of 0.3. The gesture commits when the travel is at least 40% of the panel's extent, or the velocity is at least 0.5 px/ms with at least 24 px travel, and the motion is within 33 degrees of the axis (axis ratio at least 1.5). Otherwise the panel springs back in at most 200 ms. Direction mirrors under `dir="rtl"` (an inline-start sidebar closes toward the right). A sheet's drag starts only from its handle or header, or from content that is scrolled to the top; content that can scroll MUST scroll first. Scrim tap and the visible close control remain.

#### Scenario: Sidebar swipe in English
- **WHEN** the Learner drags the sidebar 50% of its width toward the left edge and lifts
- **THEN** the sidebar closes and focus returns to the control that opened it

#### Scenario: Sidebar swipe in Arabic
- **WHEN** the locale is Arabic and the Learner drags the sidebar toward the right edge
- **THEN** it closes, and a drag toward the left edge springs back

#### Scenario: Short drag cancels
- **WHEN** the Learner drags a sheet down 20% of its height slowly and lifts
- **THEN** it springs back in at most 200 ms and nothing closes

#### Scenario: Scrolling content wins
- **WHEN** a sheet's list is scrolled below its top and the Learner swipes down on it
- **THEN** the list scrolls and the sheet does not move

#### Scenario: Flick
- **WHEN** the Learner flicks the answer panel toward its edge at 0.8 px/ms after 30 px
- **THEN** it closes although it travelled less than 40%

### Requirement: Lesson stage navigation gestures
In the lesson player (framed and immersive) a horizontal swipe on the stage SHALL go to the previous or next Slide when it travels at least 56 px within 800 ms and is within 33 degrees of horizontal; swiping left is next and swiping right is previous. A double tap within 300 ms and 48 px on the left half SHALL seek back 10 seconds and on the right half forward 10 seconds on the Lesson timeline, clamped to the Lesson start and end; each further tap within 300 ms adds 10 seconds. A seek that lands in another Slide shows that Slide at the mapped time and keeps the playing or paused state. A single tap toggles the control layer after the 300 ms double-tap wait, and the tap MUST show a pressed ripple within 100 ms. Seek and Slide gestures are not mirrored in right-to-left locales, because the progress bar and playback controls are not mirrored. A seek shows an icon with "10" and no sentence. The Slide itself MUST NOT receive these touches.

#### Scenario: Double tap forward
- **WHEN** a Slide is at 12 seconds in a Lesson and the Learner double-taps the right half
- **THEN** the Lesson jumps 10 seconds forward, the control layer is not toggled, and playback state is unchanged

#### Scenario: Repeated tap
- **WHEN** the Learner taps the right half three times within 300 ms of each other
- **THEN** the Lesson seeks 20 seconds in total after the first double tap plus one further tap, with the indicator reading 20

#### Scenario: Clamp at the end
- **WHEN** the Learner double-taps right 4 seconds before the Lesson ends
- **THEN** playback goes to the end boundary and the completion flow starts as if the Lesson had played to the end

#### Scenario: Swipe next
- **WHEN** the Learner swipes left across the stage 120 px in 200 ms
- **THEN** the next Slide is shown and a reverse swipe within 2 seconds returns to the previous Slide at its previous time

### Requirement: Hold for 2x speed
Holding one finger still on the lesson stage for 400 ms with less than 10 px movement SHALL play at 2x speed while the finger stays down, show a "2x" indicator, give a light haptic at the start, and restore the previous speed on release, cancel or loss of focus. Narration MUST follow the rate with pitch preserved. It has no effect while paused. A second finger touching during the hold cancels it and hands control to region selection. Movement over 10 px before 400 ms cancels it and is treated as a swipe.

#### Scenario: Hold and release
- **WHEN** the Learner holds the stage for 600 ms at 1x and lifts
- **THEN** playback ran at 2x from 400 ms and returns to 1x on release without toggling the control layer

### Requirement: Pull down to leave immersive mode
In immersive mode a single-finger drag that starts at least 24 px below the top edge, is within 33 degrees of vertical and moves down at least 96 px (or at least 48 px at 0.6 px/ms) SHALL leave immersive mode exactly as the back control does: the framed player shows the same Slide and time. While dragging, the stage follows the finger and scales to at most 90% with a visible exit indicator; release below the threshold springs back. It is disabled while a region is selected or the answer panel is open, and while the controls are locked.

#### Scenario: Pull to exit
- **WHEN** the Learner drags the stage down 150 px and lifts
- **THEN** the framed player is shown on the same Slide at the same time and narration continues unless it was paused

#### Scenario: Panel open
- **WHEN** the answer panel is open and the Learner drags the stage down
- **THEN** nothing exits

### Requirement: System gestures and conflicts
The gesture layer SHALL ignore touches that start within 24 CSS pixels of the left, right, top or bottom screen edge, or within the platform's reported system-gesture inset if larger, so that edge-swipe back, the home indicator and notification pull-down are never hijacked, and it MUST NOT call `preventDefault` on events that start in those margins. No surface opens by edge swipe. Conflict priority, highest first: system gesture, two-finger region selection, vertical pull exit or sheet dismiss, horizontal swipe, long-press, double tap, single tap. A gesture locks its axis after 12 px of movement and ignores the other axis for the rest of the touch. A second finger cancels any one-finger gesture in progress, restoring state. `pointercancel`, window blur, orientation change and a lock-control activation cancel everything in progress. Pinch zoom of the page stays available outside the stage; on the stage `touch-action: none` applies.

#### Scenario: Back swipe
- **WHEN** the Learner starts a swipe 10 px from the left edge in immersive mode
- **THEN** the app does not handle it and the system back gesture applies

#### Scenario: Axis lock
- **WHEN** a drag starts mostly horizontal and drifts vertical after 40 px
- **THEN** it stays a horizontal swipe and does not start a pull exit

#### Scenario: Second finger
- **WHEN** a long-press 2x is active and a second finger lands
- **THEN** speed returns to the previous value and region selection begins

### Requirement: Every gesture has an alternative
Each gesture SHALL have an equivalent that needs no gesture: a visible close control for panels and sheets, previous, next and "back 10 s" and "forward 10 s" controls and a speed control in the lesson control layer, an exit control, and keyboard shortcuts on devices with a keyboard (see `keyboard-shortcuts`). Controls MUST be reachable by screen readers and switch access with accessible names. A per-Learner setting `swipe_navigation` (default on) disables stage swipe, double tap, long-press and pull exit while keeping panel and sheet swipe-to-close; the lock control disables all of them. Gesture handlers MUST NOT be the only place where an action is announced or recorded.

#### Scenario: Screen reader user
- **WHEN** a TalkBack user opens the lesson control layer
- **THEN** "Back 10 seconds", "Forward 10 seconds", "Previous slide", "Next slide", speed and exit are focusable and operable without any swipe gesture beyond the screen reader's own

#### Scenario: Setting off
- **WHEN** `swipe_navigation` is off and the Learner swipes the stage
- **THEN** nothing happens, and swiping a sheet down still closes it

### Requirement: Gesture hints are one-time coach marks
Gestures SHALL be introduced only by first-use coach marks under the transient-explanation rules, one per gesture id (`swipe_close`, `swipe_slide`, `double_tap_seek`, `hold_2x`, `pull_exit`), shown the first time its surface is relevant and never over a playing Slide. A gesture used successfully before its hint was shown marks it as seen. No hint text is permanent. Seen ids are stored in `gesture_hints_seen` with the Learner profile and apply across devices.

#### Scenario: Already used
- **WHEN** a Learner closes the sidebar by swipe before the `swipe_close` hint was shown
- **THEN** the hint is marked seen and never appears

### Requirement: Haptics
Committed gestures (panel closed, Slide changed, seek, hold start, pull exit) SHALL give a light haptic where the platform supports it, only when the Learner's existing `haptics` setting is on and the OS allows it. The client MUST use the official Tauri mobile haptics capability verified against current Tauri documentation when implemented, fall back to `navigator.vibrate(10)` where the webview supports it, and do nothing elsewhere. Haptics MUST NOT be the only feedback.

#### Scenario: Unsupported platform
- **WHEN** the app runs in a browser without vibration support
- **THEN** gestures work with visual feedback only and no error is logged

### Requirement: Gesture reliability and mis-swipe tolerance
Gesture tracking SHALL use one animation frame per pointer move with transform-only updates and keep input-to-visual latency under 100 ms on the reference mid-range Android device. A committed Slide change or panel close MUST be undoable by the reverse gesture or the visible control, and a reverse within 2 seconds is counted as a mis-swipe by analytics. When the mis-swipe rate over 7 days exceeds 5%, the thresholds in this spec are reviewed before release. Thresholds are constants in one module with unit tests.

#### Scenario: Reverse swipe
- **WHEN** the Learner swipes to the next Slide by mistake and swipes back within 2 seconds
- **THEN** the previous Slide returns at its previous time and `gesture_used` carries `undone_within_2s=true` for the first gesture
