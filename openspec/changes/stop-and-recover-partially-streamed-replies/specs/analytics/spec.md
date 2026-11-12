## ADDED Requirements

### Requirement: Tutor stop and failure events
The catalogue SHALL include API-owned `tutor_reply_stopped` (course_id, class, reason `stop|disconnect`, delivered_chars, latency_ms) and `tutor_reply_failed` with `error_code`, `interrupted`, `retryable` and `latency_ms`, plus UUID/common properties. `$ai_generation` from tutor settlement SHALL carry the actual provider, model, `fallback_used`, token usage, `usage_available` and `usage_estimated`. Events MUST honor opt-out and MUST NOT carry message or reply text.

#### Scenario: Stopped reply
- **WHEN** a Learner stops a reply
- **THEN** `tutor_reply_stopped` carries identifiers and counts only and the allowlist drops any text property
