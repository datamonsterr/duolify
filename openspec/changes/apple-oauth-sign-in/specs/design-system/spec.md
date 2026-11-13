# Apple sign-in design-system delta

## ADDED Requirements

### Requirement: Apple sign-in interaction analytics catalogue

The design-system analytics catalogue SHALL cover Apple sign-in with the existing `ui_control_activated` event (`surface=sign_in`, `control_id=sign_in`) and cancellation `ui_recovery_selected` (`surface=sign_in`, `error_code=auth_cancelled`, `action=retry`). Delivered events MUST include a UUID identity and common properties, respect opt-out, and MUST NOT include Apple email, name, subject, authorization code, state, nonce, identity token, or learner text.

#### Scenario: Apple sign-in interaction

- **WHEN** a Learner starts Apple sign-in
- **THEN** the client emits the existing sign-in control event with only catalogued properties and common properties

#### Scenario: Apple OAuth cancellation

- **WHEN** a Learner cancels Apple sign-in
- **THEN** the client emits the catalogued cancellation recovery event only when analytics is enabled
