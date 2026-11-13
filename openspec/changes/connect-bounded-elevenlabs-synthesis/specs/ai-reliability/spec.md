## ADDED Requirements

### Requirement: Narration vendor golden rows
`lesson_narration` SHALL include multilingual, timing, vendor-failure and cost-cap golden rows backed by the recorded fixture `evals/fixtures/narration.vendor.json`, replayed against a local fake server. Live promotion stays disabled until E10-06.

#### Scenario: Cost cap
- **WHEN** a request asks for five attempts and a 120 s timeout
- **THEN** the Engine clamps to two attempts and 60 s and charges only delivered characters
