## ADDED Requirements

### Requirement: Private transcription route
The transcription route SHALL use qwen/qwen3-asr-0.6b with openai/whisper-large-v3-turbo as fallback via OpenRouter with zero-data-retention and data collection denied.

#### Scenario: Primary transcription fails
- **WHEN** the primary route fails after the client level gate
- **THEN** the configured fallback is available

### Requirement: Generation truncation is observable
Preview and Lesson generation SHALL log a clear length or truncation reason when provider output is cut, without logging output content.

#### Scenario: Provider stops at output limit
- **WHEN** a preview or Lesson completion is truncated
- **THEN** logs identify the length termination
