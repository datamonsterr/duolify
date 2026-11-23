## ADDED Requirements

### Requirement: Voice note button
The immersive side rail and the non-immersive lesson player controls SHALL show one icon-only round Voice note button, visually distinct from the Ask microphone. Holding it records, releasing saves a Quick note, and no panel, sheet or sidebar opens at any point. The Lesson MUST pause while recording and resume afterwards only if the Voice note paused it. The rail MUST NOT keep a Note button that opens the Quick note panel; typed notes stay reachable from the answer panel and the Notes page.

#### Scenario: Hold, speak, release
- **WHEN** the Learner holds the Voice note button, speaks and releases
- **THEN** the Lesson pauses while held, no panel opens, a toast says "Saved" with Undo and the Lesson resumes

#### Scenario: Learner pressed Play meanwhile
- **WHEN** the Learner starts playback while recording and then releases
- **THEN** the Voice note does not toggle playback again
