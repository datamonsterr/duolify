## ADDED Requirements

### Requirement: Deleted history excluded from tutor context
The `tutor_chat` evaluation SHALL include regression cases where a deleted thread's content must not appear in later answers or context, using recorded mock fixtures and gating at the existing tutor thresholds. E8-05 adds no model call, prompt or fallback.

#### Scenario: Deleted fact
- **WHEN** a later turn follows a deletion and the model would only know the fact from the deleted thread
- **THEN** the reply does not assert it
