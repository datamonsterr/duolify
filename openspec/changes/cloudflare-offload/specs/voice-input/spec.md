## ADDED Requirements

### Requirement: Private transcription through AI Gateway
When the configured Gateway transcription route has verified compatibility, the Engine MAY send the existing OpenRouter transcription request through it. Every attempt MUST retain `provider.zdr=true` and `provider.data_collection="deny"`. The Gateway MUST skip log collection and cache, and audio and transcript payloads MUST be excluded from diagnostic and trace storage. An unverified Gateway transcription path SHALL use direct OpenRouter. A Gateway failure MAY use the bounded direct fallback only under the same privacy flags, per-request cost cap and deadline.

#### Scenario: Gateway supports transcription
- **WHEN** the verified Gateway route transcribes a fixture recording
- **THEN** both requests' privacy controls are enforced and the pipeline records only content-free telemetry

#### Scenario: Gateway route unverified
- **WHEN** the Gateway cannot be verified for the existing transcription contract
- **THEN** the Engine uses direct OpenRouter with the existing ZDR settings
