## ADDED Requirements

### Requirement: Explicit narration playback
The player SHALL start narration audio only after the Learner presses "Play narration", never automatically. It MUST show captions marked in sync with the alignment, offer Pause, and fall back to "Narration unavailable" with captions and transcript when status is not `ready` or the audio or alignment is invalid.

#### Scenario: Press Play
- **WHEN** a slide narration is ready and the Learner presses Play narration
- **THEN** audio starts, the spoken characters are marked in the captions and `narration_played` is emitted

#### Scenario: Narration unavailable
- **WHEN** narration is failed or invalid
- **THEN** the player shows "Narration unavailable", keeps captions and the animation, and the Learner can continue to the lesson test
