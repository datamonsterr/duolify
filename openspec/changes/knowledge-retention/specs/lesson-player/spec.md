## ADDED Requirements

### Requirement: Checkpoint interaction cards
A Lesson SHALL carry up to three interactions bound to Slide checkpoints: `guess_first` (one question with 3 or 4 choices, before the concept Slide), `explain_why` (one prompt with 2 or 3 key ideas and a model answer, after the worked example) and `free_recall` (the Lesson key points, after the last Slide). At the checkpoint the player SHALL pause the Slide and narration and show a native card outside the sandboxed frame. Every card SHALL have Skip, and none MUST gate the Lesson test or completion. `explain_why` and `free_recall` accept typed text or voice input and show the graded coverage, or a self-check list when grading is unavailable.

#### Scenario: Guess first wrong
- **WHEN** a Learner picks a wrong guess
- **THEN** the card shows "Let's find out", the Slide resumes and no score or Credits change

#### Scenario: Grading unavailable
- **WHEN** the grader fails after one repair
- **THEN** the card shows the model answer with the key ideas as a checklist the Learner ticks

#### Scenario: Offline
- **WHEN** the device is offline
- **THEN** `guess_first` works and free-text cards show the self-check list

### Requirement: Motion timeline runtime
A Slide MAY carry a `timeline` of at most 24 steps `{at, target, effect, dur}`. `effect` is one of `reveal`, `fade_out`, `slide_in`, `highlight`, `underline`, `draw`, `pulse`, `count_up`, `typewriter`, `move_to`, `dim_others`. `at` is milliseconds within `duration_ms` or `{cue: word_index}` resolved through the narration alignment, falling back to the word's proportional position in the narration text. A trusted runtime bundled with the player SHALL compile the timeline to Web Animations API animations, so `play`, `pause`, `seek` and `rate` control them like CSS animations. Generated Slides MUST NOT contain scripts; the timeline is data only. With reduced motion, each step SHALL jump to its end state.

#### Scenario: Seek backwards
- **WHEN** the Learner seeks to before a `draw` step
- **THEN** the stroke shows undrawn and redraws on play

#### Scenario: Unknown target
- **WHEN** a timeline step targets an id not in the Slide
- **THEN** Engine validation rejects the Lesson for repair before storage
