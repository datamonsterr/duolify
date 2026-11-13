## ADDED Requirements

### Requirement: Tutor context and cache properties
`tutor_reply_completed` SHALL carry `envelope` (`short|long`), `cache_class` (`cold|warm|none`), `summary_count`, `excerpt_count`, `context_tokens`, `cache_read_tokens` and `cache_write_tokens`; tutor `$ai_generation` SHALL carry `envelope` and `cache_class`. Properties are ids and counts; prompts, excerpts and replies MUST NOT be sent, and opt-out is honoured.

#### Scenario: Cold-cache reply
- **WHEN** the provider reports cache writes and no reads
- **THEN** the events carry `cache_class=cold` and counts only
