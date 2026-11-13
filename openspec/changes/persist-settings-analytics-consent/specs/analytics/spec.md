## ADDED Requirements

### Requirement: Consent-aware client analytics boundary
The client SHALL identify learners only by internal UUID and include platform, app_version and source on allowlisted events. Opting out SHALL immediately stop captures and clear pending learner events. Analytics failures SHALL NOT block settings updates or other requests.

#### Scenario: Opt out immediately
- **WHEN** a learner turns analytics off
- **THEN** no subsequent client or API event is delivered for that learner and pending client events are cleared

#### Scenario: Settings events
- **WHEN** an opted-in learner changes appearance
- **THEN** `ui_appearance_changed` contains only the stable appearance enum and common properties

### Requirement: Consent event catalog
The analytics catalogue SHALL include `analytics_opt_out_changed` and `ui_appearance_changed`. The opt-out transition event SHALL be delivered only when the learner remains opted in; opting out suppresses its delivery. No preference values that expose profile or learner text SHALL be sent.

#### Scenario: Opt-out transition
- **WHEN** a learner turns analytics off
- **THEN** capture is stopped and no opt-out event is transmitted after the change

### Requirement: Analytics backup and retention
PostHog SHALL be a derived store and SHALL NOT be the only copy of a preference or consent value. The source values SHALL remain in the backed-up learner profile and be deleted with that row.

#### Scenario: Delete analytics preferences
- **WHEN** a Learner profile is deleted
- **THEN** its persisted consent and settings are removed with the profile
