## ADDED Requirements

### Requirement: Mascot corner companion in the framed player
The framed lesson player SHALL host a 48 px mascot companion in its chrome outside the slide viewport, reacting presentation-only to events (Lesson passed, streak milestone, hint). It MUST be hidden in immersive mode unless speaking, MUST be hideable in Settings, MUST NOT overlap slides or controls, MUST NOT enter the sandboxed webview, and MUST NOT change playback, grading or generation.

#### Scenario: Slide playing
- **WHEN** a slide plays in the framed player
- **THEN** the companion stays in the chrome, idle, and no slide pixel is covered
