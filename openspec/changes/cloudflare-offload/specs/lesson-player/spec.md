## ADDED Requirements

### Requirement: Playback with estimated narration alignment
For valid narration audio with estimated character alignment, the Lesson player SHALL use the existing alignment contract and single controlled media clock. Captions and checkpoints MUST remain monotonic across play, pause, seek, speed changes and replay. Estimated highlights MUST use decoded duration, preserve the full transcript, and end by the audio endpoint. The animation SHALL hold its final frame until audio completes. Corrupt audio, missing alignment or offline cache miss MUST leave captions and the Slide usable.

#### Scenario: Estimated alignment and seek
- **WHEN** a Learner plays a Slide with estimated timings and changes playback speed or seeks
- **THEN** audio, animation and caption position follow the same clock and the final highlight ends with the audio

#### Scenario: Cached narration missing or invalid
- **WHEN** the Lesson is opened offline without valid audio and alignment
- **THEN** captions and Lesson progression remain usable without synthesis or a charge
