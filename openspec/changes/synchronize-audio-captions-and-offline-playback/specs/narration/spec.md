## ADDED Requirements

### Requirement: Synchronized narration clock and offline caching
Narration audio and alignment data SHALL integrate with the player's master clock. Audio playback rate MUST follow the user-selected playback speed (0.75, 1, 1.25, 1.5). When a lesson is opened, narration audio and alignment for upcoming slides MUST be preloaded and cached in client storage. Offline restart MUST utilize cached narration assets when present, and fall back to caption display when absent without charging credits.

#### Scenario: Preload next slide narration
- **WHEN** slide N is playing
- **THEN** audio and alignment for slide N+1 are preloaded into client cache

#### Scenario: Speed control affects narration
- **WHEN** the Learner changes playback speed to 1.25x
- **THEN** audio playback rate and slide animation rate update simultaneously
