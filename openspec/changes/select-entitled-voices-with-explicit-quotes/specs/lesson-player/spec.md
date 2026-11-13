## ADDED Requirements

### Requirement: Voice panel and audio-credit recovery
The player SHALL offer a voice panel that lists approved voices, shows the new character quote with Confirm and Decline before any synthesis, and keeps the current narration playable while the new voice is prepared. When audio credits are insufficient the player MUST disable only audio, say so, and offer Retry narration and Add credits without blocking text, captions or the Lesson test. Controls MUST be keyboard and touch reachable with 44px targets on mobile.

#### Scenario: Decline on mobile
- **WHEN** the Learner opens the voice panel at 390px, reads the quote and declines
- **THEN** the panel closes, the old narration still plays and no credits change

#### Scenario: Insufficient audio credits
- **WHEN** a slide's audio is disabled for insufficient credits
- **THEN** the text and test remain usable and Retry narration succeeds after credits arrive
