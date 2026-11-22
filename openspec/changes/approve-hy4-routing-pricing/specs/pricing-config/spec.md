## ADDED Requirements

### Requirement: Owner-approved Hy4/GLM routing version
An immutable approved Pricing version SHALL quote every AI action on `tencent/hy4-flash` with `z-ai/glm-5.3-flash` as fallback, both through `openrouter`, with credit prices, packs, plans and limits identical to the previous approved snapshot. It SHALL be the active quote version once its effective instant has passed, and the unapproved proposal row SHALL remain unapproved and never active.

#### Scenario: Preview quotes the served route
- **WHEN** a learner requests a preview quote after migrations
- **THEN** the quote names the approved version, `openrouter/tencent/hy4-flash` and its Hy4 rate, and settlement of Hy4 or GLM usage charges that model's own rate while a Claude model is rejected

#### Scenario: Active version matches the engine
- **WHEN** CI compares the active approved version with `engine/config/models.toml` and the startup route check runs with engine providers `openrouter`/`openrouter`
- **THEN** no action is reported as unserved

#### Scenario: Later learner price changes
- **WHEN** a later version changes a learner-facing price
- **THEN** it still goes through draft, owner approval and the 72-hour adverse-notice rule
