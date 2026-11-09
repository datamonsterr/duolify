## MODIFIED Requirements

### Requirement: Per-course level
A Learner SHALL be able to set a level per Course that overrides the global level for that Course, and reset it to revert to the global profile level. Setting or resetting a per-course level MUST NOT alter or overwrite the Learner's global profile level stored in `learner_profiles`. The API context builder SHALL project an effective profile combining the global profile with the Course's level override for AI engine requests.

#### Scenario: Course override
- **WHEN** a Course level is set
- **THEN** AI requests for that Course use the Course level
- **AND** the global profile level remains unchanged

#### Scenario: Course level reset
- **WHEN** a Course level override is reset
- **THEN** AI requests for that Course fall back to the global profile level
- **AND** the global profile level remains unchanged
