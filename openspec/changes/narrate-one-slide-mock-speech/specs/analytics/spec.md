## ADDED Requirements

### Requirement: Narration analytics events
The catalogue SHALL define `narration_generated` (lesson_id, slide_id, characters, voice_id, model, latency_ms), `narration_cache_hit` (lesson_id, slide_id, model) and `narration_failed` (lesson_id, slide_id, fixed error_code) from the API, and `narration_played` (lesson_id, slide_id, duration_ms) from the client. Synthesis usage SHALL be reported as `$ai_generation` with `feature` `narration`, `narration_characters`, provider, model and latency. Events MUST carry the common properties and a UUID distinct id, MUST be dropped when the Learner opted out, and MUST NOT contain narration text, captions, audio or URLs. Catalogue entries precede emitters in the same change.

#### Scenario: Generated without private text
- **WHEN** synthesis succeeds
- **THEN** PostHog receives `narration_generated` and `$ai_generation(feature=narration)` with counts and ids and no narration text

#### Scenario: Opt-out
- **WHEN** the Learner opted out
- **THEN** no narration event is delivered
