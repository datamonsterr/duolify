## ADDED Requirements

### Requirement: Single shortcut registry
All keyboard shortcuts SHALL be declared once in a typed registry (id, key combination, scope, action, label message id, platforms) from which the handlers, the help overlay, `aria-keyshortcuts` attributes, tooltips and the exported Tauri menu accelerator table are generated. A shortcut MUST NOT be handled outside the registry. Letter shortcuts match the physical key (`event.code`, so they work on AZERTY and Arabic layouts) and symbol shortcuts match the produced character (`event.key`). On macOS the primary modifier is Command and glyphs are shown; elsewhere it is Control.

#### Scenario: Registry drives everything
- **WHEN** a shortcut is added to the registry
- **THEN** it appears in the help overlay, its control gets `aria-keyshortcuts`, and a test fails if a handler exists without a registry entry

#### Scenario: Arabic keyboard layout
- **WHEN** the active layout produces Arabic letters and the Learner presses the physical K key in the lesson player
- **THEN** playback toggles

### Requirement: Lesson player shortcuts
While the Lesson player has focus (framed or immersive) and no text field is focused, these shortcuts SHALL work, following mainstream video-player conventions: Space and K play or pause; Left and Right seek back or forward 5 seconds; J and L seek back or forward 10 seconds; Shift+Left, Shift+Right, PageUp and PageDown go to the previous or next Slide; Home and End go to the Lesson start or end; 0 to 9 seek to 0% to 90% of the Lesson; Up and Down change Narration volume by 10%; M mutes; C toggles captions; Shift+, and Shift+. (less-than and greater-than) step the speed through 0.5, 0.75, 1, 1.25, 1.5 and 2; F enters or leaves immersive mode and, on desktop, native full screen; Esc leaves immersive mode or closes the topmost layer; N opens a quick note anchored to the current Slide and time; B bookmarks the Slide; R starts keyboard region selection; slash focuses the ask field; question mark opens the shortcut help. Space MUST NOT toggle playback when a button, link or slider has focus (it activates that control). Keys with a focused slider keep the slider's native behaviour. Seeking shortcuts use the same Lesson-timeline semantics as double tap, and Left and Right are not mirrored in right-to-left locales.

#### Scenario: Pause with space
- **WHEN** a Slide is playing, focus is on the stage and the Learner presses Space
- **THEN** playback pauses within 100 ms and the control layer shows the paused state

#### Scenario: Seek percent
- **WHEN** the Learner presses 5
- **THEN** the Lesson jumps to 50% of its duration

#### Scenario: Button focus
- **WHEN** focus is on the "Captions" button and the Learner presses Space
- **THEN** captions toggle and playback state does not change

#### Scenario: Speed
- **WHEN** speed is 1x and the Learner presses Shift+.
- **THEN** speed becomes 1.25x and a toast reads "1.25x"

### Requirement: App-wide shortcuts
Outside the Lesson player the client SHALL support: Escape closes the topmost layer only (tooltip, coach mark, menu, then sheet or dialog, then side panel, then immersive mode), one layer per press, and never navigates back; Control or Command with comma opens Settings; Control or Command with 1 to 4 opens the primary destinations in tab order, bound in the desktop app only; question mark opens the shortcut help; Control or Command with Z activates the Undo of a visible undo toast. A command palette (Control or Command with K) is NOT included in this change. Shortcuts that duplicate the browser (Control with T, W, N, R, L, F5, Alt with Left or Right) and platform keys MUST NOT be bound, and the web build binds only the shortcuts that are inactive in mainstream browsers (the app-wide Control or Command with comma and with digits are desktop-app-only).

#### Scenario: Layered Escape
- **WHEN** a sheet is open above an immersive Lesson and a tooltip is showing
- **THEN** the first Escape closes the tooltip, the second the sheet, the third leaves immersive mode

#### Scenario: Settings
- **WHEN** the Learner presses Command and comma in the macOS desktop app
- **THEN** Settings open, and the same entry exists in the application menu with that accelerator

#### Scenario: Not bound in web
- **WHEN** the web build receives Control and 2
- **THEN** the browser's own behaviour applies and the app does nothing

### Requirement: Text entry and IME safety
Single-key shortcuts SHALL be ignored when the event target is an input, textarea, select, `contenteditable` or an element with role `textbox`, `combobox` or `searchbox`, when any of Control, Alt or Meta is down (except registry entries that use them), and during IME composition (`isComposing` or `keyCode` 229). In multi-line composers (tutor ask, answer panel, quick note) Enter sends and Shift+Enter inserts a newline on devices with a fine pointer; Control or Command with Enter also sends; on coarse-pointer devices Enter inserts a newline and the visible send control sends. Enter MUST NOT send while an IME composition is active, while the composer is empty or while a send is pending. Auto-repeat MUST NOT repeat toggles.

#### Scenario: Japanese IME
- **WHEN** the Learner presses Enter to confirm an IME candidate in the ask field
- **THEN** the candidate is committed and nothing is sent

#### Scenario: Typing letters
- **WHEN** the Learner types "kmc" in the ask field
- **THEN** the text appears and no player action runs

#### Scenario: Newline
- **WHEN** the Learner presses Shift+Enter in the ask field
- **THEN** a newline is inserted and nothing is sent

### Requirement: Accessible and non-conflicting shortcuts
Single-character shortcuts SHALL satisfy WCAG 2.1.4: they are active only while the Lesson player has focus, and a per-Learner setting `single_key_shortcuts` (default on) turns them all off; modifier shortcuts are unaffected. Shortcuts MUST NOT use keys reserved by screen readers (Insert, CapsLock, Control+Option on macOS), the operating system (Windows or Super key, Alt+Tab, Command+Tab, Command+Q) or the Tauri webview defaults (F11 and F12 are left to the platform). Every control that has a shortcut MUST expose it through `aria-keyshortcuts` and its tooltip. Every shortcut action MUST also be reachable by a visible control. Shortcuts are not remappable in v1.

#### Scenario: Turn off single keys
- **WHEN** the Learner turns off single-key shortcuts and presses K in the player
- **THEN** nothing happens, while Escape and Control or Command with comma still work

#### Scenario: Attribute
- **WHEN** the play control renders
- **THEN** it carries `aria-keyshortcuts="K Space"` and its tooltip shows "Play (K)"

### Requirement: Shortcut help overlay
Pressing question mark, choosing Help then Keyboard shortcuts in the desktop menu, or activating the keyboard icon in Settings SHALL open a dialog listing the registry for the current screen first and then the app-wide shortcuts, grouped as Playback, Navigate, Notes and ask, and App, at most 24 rows. Each row is the key combination in `kbd` elements and an action label of at most 3 words; there is no paragraph. The dialog traps focus, closes with Escape or its close control, and is a bottom sheet on phones with a hardware keyboard attached. It MUST NOT open while a text field is focused.

#### Scenario: Open and close
- **WHEN** the Learner presses Shift+/ on the lesson screen
- **THEN** the overlay lists playback shortcuts first, and Escape closes it and returns focus

### Requirement: Desktop menu consistency
The Tauri desktop menu SHALL expose the shortcuts that have native equivalents (Settings with Command or Control and comma, destinations with Command or Control and 1 to 4, Toggle full screen, Keyboard shortcuts, standard Edit items) with the same accelerators as the registry, generated from or checked against the registry's exported table by a test in `mise run client:test`. A key pressed in the menu and in the webview MUST NOT run the action twice.

#### Scenario: Drift
- **WHEN** an accelerator in the Rust menu differs from the registry
- **THEN** the consistency test fails

#### Scenario: No double fire
- **WHEN** the Learner presses Command and comma
- **THEN** Settings open once
