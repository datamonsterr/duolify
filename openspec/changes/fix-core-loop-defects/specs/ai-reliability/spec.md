## ADDED Requirements

### Requirement: Quote route coverage is visible
The API and worker SHALL log an error at startup for every quoted action whose active pricing
route the Engine will refuse, naming the action, the pricing version, the quoted route and the
Engine route. CI MUST fail when the Engine's task routes in `engine/config/models.toml` are not
covered by the newest routing quote policy proposal. Neither check changes quoting or serving.

#### Scenario: Active pricing quotes a retired route
- **WHEN** the active pricing version quotes a provider the Engine does not serve
- **THEN** startup logs an error per affected action instead of the first learner seeing a 502

#### Scenario: Engine route changes without a matching pricing proposal
- **WHEN** `engine/config/models.toml` routes a quoted task to a model the newest routing proposal does not quote
- **THEN** the API test suite fails and names the action and both routes
