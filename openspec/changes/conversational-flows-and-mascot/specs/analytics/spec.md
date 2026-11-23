## ADDED Requirements

### Requirement: Guided chat and mascot event catalogue
The catalogue SHALL include `guided_chat_started`, `guided_chat_resumed`, `guided_chat_step_answered`, `guided_chat_step_edited`, `guided_chat_free_text_asked`, `guided_chat_abandoned`, `guided_chat_completed`, `mascot_customized`, `mascot_companion_toggled` and `mascot_state_fallback` with the properties defined in the `guided-chat` and `mascot` specs. Feature attribution MUST use `entry_point`, never the common `source` property. Events MUST be content-free (no Learner text, FAQ content or secret values) and follow the identity, opt-out and common property rules. Dashboards SHALL report guided chat completion rate versus the replaced form, time to complete, drop-off step and free-text rate per script.

#### Scenario: Attribution
- **WHEN** `guided_chat_started` is emitted from onboarding
- **THEN** it carries `entry_point=onboarding` and the common `source=client`

#### Scenario: Drop-off report
- **WHEN** a script's funnel is queried
- **THEN** it lists the last `step_id` of each abandoned run without any answer content
