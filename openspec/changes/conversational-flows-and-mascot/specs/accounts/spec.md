## ADDED Requirements

### Requirement: Sign-in as guided chat with real inputs
Sign-in and age confirmation SHALL be the guided chats `auth.signin` and `auth.age`. The email and one-time code steps MUST render real labelled `<input>` elements (`type="email"` with `autocomplete="email"`, `autocomplete="one-time-code"` with numeric `inputmode`) inside the chat so autofill, password managers and screen readers work. Codes and passwords MUST NOT be echoed in bubbles, drafted, logged or sent to analytics. Existing return-path allowlist, resend, change-email, cancel, throttle, offline and error behaviours MUST be preserved, and the pre-auth chat state MUST be memory-only. Account deletion MUST remain an explicit destructive confirm card that free text cannot confirm.

#### Scenario: Wrong code
- **WHEN** the Learner enters an invalid code
- **THEN** an inline error appears on the code input, the mascot shows `confused`, and the throttle and resend rules are unchanged

#### Scenario: Pre-auth state
- **WHEN** the app restarts during sign-in
- **THEN** no sign-in answers are restored from storage and the chat starts again at email
