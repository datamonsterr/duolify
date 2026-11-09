## ADDED Requirements

### Requirement: Session recovery and device controls
Session screens SHALL follow S01 and S23 with token colors, light/dark and reduced-motion support, mobile step screens, keyboard focus, wrapping content, loading, empty and offline/error recovery states. Analytics catalogue: ui_recovery_selected(surface=sign_in,error_code=session_expired,action=sign_in). The existing PostHog wrapper MUST send only those stable properties plus verified UUID, platform, app_version and source, honor both opt-out boundaries and exclude private text, tokens and destinations.

#### Scenario: Recovery selection
- **WHEN** a known learner selects Sign in again after session expiry
- **THEN** the opted-in PostHog test double receives the catalogued common properties without private content and the email-code step receives focus
