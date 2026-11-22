## ADDED Requirements

### Requirement: Transcription and region tasks are evaluated and gated
The Engine SHALL treat transcription, region-grounded lesson Tutor chat and Slide region generation as AI tasks with their own Golden set cases, thresholds, timeouts, fallback and cost ceilings. Each task MUST run on a live Engine only with promotion evidence for that task, and MUST answer with the typed retryable unavailable reason otherwise. Transcription usage MUST be reported with served model, fallback flag, latency and cost like other generations, and MUST carry no audio or transcript text in analytics.

#### Scenario: Region generation regresses
- **WHEN** a build's live eval shows region descriptions failing the threshold
- **THEN** that build has no promotion evidence for Lesson generation with regions and the feature stays closed

#### Scenario: Transcription not promoted
- **WHEN** a live Engine has no promotion evidence for transcription
- **THEN** the transcription RPC answers with the typed unavailable reason and makes no provider call
