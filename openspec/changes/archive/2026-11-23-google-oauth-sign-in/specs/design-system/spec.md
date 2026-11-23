## ADDED Requirements

### Requirement: Google sign-in interaction analytics catalogue

The design-system analytics catalogue SHALL define the Google sign-in `ui_control_activated` event with `surface=sign_in` and `control_id=sign_in`, and the cancellation `ui_recovery_selected` event with `surface=sign_in`, `error_code=auth_cancelled`, and `action=retry`. Events MUST include a UUID identity and common properties when delivered, respect opt-out, and MUST NOT include email, authorization code, state, token, provider subject, or learner text.

#### Scenario: Google sign-in interaction

- **WHEN** a Learner starts Google sign-in
- **THEN** the client emits `ui_control_activated` with only the catalogued control and common properties

#### Scenario: OAuth cancellation

- **WHEN** a Learner cancels Google sign-in
- **THEN** the client emits `ui_recovery_selected` with the fixed cancellation code and retry action, if analytics is enabled
