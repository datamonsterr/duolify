## MODIFIED Requirements

### Requirement: Microphone permission and unavailable states
The client SHALL request microphone permission only when the Learner first uses a microphone control. With permission denied, offline, voice not enabled for the environment, or the daily cap reached, the microphone control MUST be disabled or show the state, and typing MUST remain available. States, explanations and failures (permission needed or denied, offline, daily cap with its reset time, voice unavailable, recording too short or cancelled, nothing understood, errors) MUST be shown as toasts, with at most one action (Retry, Settings), and MUST NOT be written as paragraphs inside the answer panel or Quick note panel. A disabled control MUST still expose the reason through its description. Voice MUST NOT be the only way to ask or to take a note. In immersive mode the microphone controls for Talk and Note MUST be small round icon bubbles that show the recording state (pulse or level ring and a timer) on the bubble itself.

#### Scenario: Permission denied
- **WHEN** the Learner denies microphone permission
- **THEN** a toast with a Settings action says so and the text composer stays usable

#### Scenario: Offline
- **WHEN** the device is offline
- **THEN** a toast says voice needs a connection, the bubble is disabled with the reason as its description, and typed Quick notes still queue offline

#### Scenario: Daily cap
- **WHEN** the daily voice cap is reached
- **THEN** a toast shows when it resets

### Requirement: Transcript review before use
A transcript for a question SHALL be shown in the composer and sent automatically only after a 2-second window in which the Learner can edit or cancel, shown as a countdown chip with a Cancel control; a Learner setting in the player settings MUST allow requiring an explicit send, and the answer panel MUST NOT show a checkbox for it. The setting is on by default, except that it MUST default to off when the transcript language is Vietnamese. A transcript for a Quick note SHALL be saved immediately as a raw Quick note with an undo action in a toast. An empty or failed transcription MUST NOT create a message or a note and MUST show a toast offering retry.

#### Scenario: Edit before sending
- **WHEN** the transcript appears and the Learner changes a word within 2 seconds
- **THEN** automatic sending stops and the edited text is sent only when the Learner sends it

#### Scenario: Nothing understood
- **WHEN** transcription returns no text
- **THEN** no message or note is created and a toast offers retry
