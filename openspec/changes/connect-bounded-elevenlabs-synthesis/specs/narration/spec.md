## ADDED Requirements

### Requirement: Bounded vendor synthesis
The Engine SHALL synthesize narration through ElevenLabs only when `ELEVENLABS_API_KEY` is set AND `NARRATION_LIVE_ENABLED=true` with a configured voice; otherwise it MUST use the mock. Each call SHALL be limited to 60 seconds and one repair/retry after the primary within the quote caps. Output MUST cover every character with non-decreasing timestamps ending near the audio duration, else it is rejected. A terminal vendor failure MUST release the undelivered reservation, charge nothing and leave captions as the only fallback.

#### Scenario: Vendor outage
- **WHEN** the vendor answers 503 on the primary and the retry
- **THEN** the slide is `failed` with captions available, Retry is offered, nothing is charged and no narration text appears in events or errors

#### Scenario: Permanent rejection
- **WHEN** the vendor answers 401 or 422
- **THEN** no retry is made
