## ADDED Requirements

### Requirement: Preview evaluation matches the served envelope
The golden evaluation SHALL use the same configured preview output limit as server generation.

#### Scenario: Preview token cap changes
- **WHEN** the active configured output limit changes
- **THEN** golden evaluation applies that same limit
