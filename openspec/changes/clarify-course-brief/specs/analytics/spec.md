## ADDED Requirements

### Requirement: Course brief privacy-safe event catalogue
The client SHALL emit existing `ui_control_activated` for `quick_reply`, `skip`, `form_fallback` through the guided-chat surface and `confirm_brief` through the course_brief surface. Mobile opening SHALL emit `ui_sheet_opened(sheet_id=course_brief)`. The API SHALL emit `$ai_generation(task=course_brief_chat)` per attempt with pricing_version, provider, model, token counts, latency_ms, fallback_used, succeeded and usage_available. Events MUST use authenticated Learner UUID and common platform/app_version/source properties, honor opt-out and remain non-blocking. Unknown usage MUST be labelled unavailable. Learner text, brief values and transcripts MUST NOT enter analytics. Paid preview events remain outside clarification.

#### Scenario: Learner confirms a brief
- **WHEN** a signed-in opted-in Learner selects Confirm brief
- **THEN** ui_control_activated contains control_id=confirm_brief and surface=course_brief with common properties
- **AND** no brief text is captured

#### Scenario: Opted-out learner clarifies
- **WHEN** the Learner has opted out
- **THEN** neither guided controls nor model attempts produce a PostHog event
