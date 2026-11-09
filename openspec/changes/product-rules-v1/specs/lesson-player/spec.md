## MODIFIED Requirements

### Requirement: Slide format and manifest
Each Slide MUST be one self-contained Motion HTML document (HTML, CSS, JS) that plays a few seconds. A Lesson MUST have a Slide manifest listing Slides in order with `slide_id`, `duration_ms`, asset URLs, checkpoints and narration fields (`narration_text`, `narration_status`, `audio_url` and `alignment_url` when status is `ready`). The Engine and API MUST validate Slide HTML before it is stored.

#### Scenario: Valid Lesson stored
- **WHEN** the Engine returns a Lesson
- **THEN** the API stores each Slide in object storage and writes the Slide manifest

#### Scenario: Invalid Slide HTML
- **WHEN** a Slide references an external network origin other than the asset origin
- **THEN** validation fails and the Slide is regenerated or the job fails

### Requirement: Playback protocol and controls
The player SHALL control a Slide through a `postMessage` protocol with `play`, `pause`, `seek`, `rate`, `ended` and `checkpoint`. The Learner MUST be able to pause, replay and step to the previous or next Slide, and to change playback speed. When Narration plays, these controls MUST drive the audio and the Slide together.

#### Scenario: Pause and replay
- **WHEN** the Learner taps pause and then replay
- **THEN** the Slide and its audio pause at the current time, then restart from zero

#### Scenario: Slide ends
- **WHEN** a Slide posts `ended` and its audio, if any, has finished
- **THEN** the player shows the next-step control and advances on tap or after auto-advance if enabled

### Requirement: Player analytics events
The client SHALL emit `lesson_started`, `slide_viewed`, `slide_replayed`, `slide_paused`, `lesson_completed`, `lesson_player_error`, `narration_played`, `narration_muted`, `narration_speed_changed` and `captions_toggled` with properties from the `analytics` spec.

#### Scenario: Slide viewed
- **WHEN** a Slide reaches `ended`
- **THEN** `slide_viewed` is emitted with `lesson_id`, `slide_id`, `index`, `watch_ms` and `offline`

## ADDED Requirements

### Requirement: Narration playback
When a Slide has `narration_status` `ready`, the player SHALL play its audio in sync with the Slide animation, using the character alignment to fire Slide checkpoints and to highlight captions. The Slide MUST hold its last frame until the audio ends, and the audio MUST stop when the Learner leaves the Slide. The player MUST provide captions (on by default), a mute toggle and a speed control (0.75, 1, 1.25, 1.5), apply the speed to audio and Slide together, and persist these settings per device. Audio and alignment MUST be preloaded for the next Slide and MUST be stored in the offline cache with the Lesson. When `narration_status` is `pending`, `failed` or `disabled`, or audio fails to load, the player MUST show captions from `narration_text` only and the Lesson MUST stay playable. Muting MUST NOT affect captions.

#### Scenario: Audio in sync
- **WHEN** a Slide with ready narration plays
- **THEN** audio starts with the Slide, captions highlight with the alignment, and checkpoints fire at their aligned times

#### Scenario: Mute
- **WHEN** the Learner mutes
- **THEN** audio is silent, captions stay, and the setting persists for the next Slide and session

#### Scenario: Audio missing
- **WHEN** the audio object fails to load or `narration_status` is `failed`
- **THEN** captions are shown, the Slide plays and the Lesson continues

#### Scenario: Offline
- **WHEN** a cached Lesson is opened offline
- **THEN** its narration audio plays from the cache
