## ADDED Requirements

### Requirement: Synchronized playback and clock control
The player SHALL drive both slide Motion HTML animation and narration audio from one learner-controlled media clock. Play, pause, seek, replay, and playback speeds (0.75, 1, 1.25, 1.5) MUST affect both animation and audio together. Checkpoints and caption highlights MUST align with character alignment timestamps. The slide MUST hold its final frame until narration audio completes, and audio MUST stop immediately when the Learner leaves the slide.

#### Scenario: Synchronized play and seek
- **WHEN** the Learner plays, pauses, seeks, or replays the slide
- **THEN** both slide animation and narration audio move in lockstep to the target playback time

#### Scenario: Hold final frame through audio end
- **WHEN** the slide animation duration is shorter than the narration audio
- **THEN** the slide holds its final frame until the audio finishes before advancing

### Requirement: Resilient audio playback and offline fallback
The player MUST provide captions (defaulting to ON), a mute toggle, and a speed selector. Muting audio MUST NOT hide captions. Per-device settings for speed, mute, and captions MUST persist. Audio and alignment for the next slide MUST be preloaded and cached for offline restart. When narration audio is pending, failed, disabled, or encounters a network error, the player MUST fall back to caption-only playback without interrupting slide animation or lesson completion.

#### Scenario: Offline restart with cached audio
- **WHEN** a cached lesson is reopened offline
- **THEN** preloaded audio and alignment play from cache

#### Scenario: Missing audio fallback
- **WHEN** narration audio is unavailable or fails to load
- **THEN** the slide displays captions and continues normal playback
