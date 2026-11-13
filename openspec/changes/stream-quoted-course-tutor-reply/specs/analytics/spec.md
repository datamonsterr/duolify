## ADDED Requirements

### Requirement: Tutor turn events
The catalogue SHALL include API-owned `tutor_message_sent` (course_id, class, message_length), `tutor_reply_completed` (course_id, class, model, latency_ms, input_tokens, output_tokens) and `tutor_reply_failed` (course_id, class, error_code, interrupted, latency_ms), plus UUID/common properties. Events MUST honor current opt-out and MUST NOT carry message or reply text. Each settled tutor reply SHALL also produce the existing `$ai_generation` event from credit settlement.

#### Scenario: Private tutor turn
- **WHEN** a reply completes or is interrupted
- **THEN** the allowlisted event carries identifiers and counts only
