## ADDED Requirements

### Requirement: Curated voices need an entitlement and an explicit quote
A Learner SHALL choose a narration voice only from the approved voice set in config (the default voice plus test fixtures). A non-default voice MUST require the verified funded-plan benefit "Curated voices at the same synthesis rate". Before any synthesis in a different voice the API MUST disclose a quote (characters, credits, Pricing version, voice-config hash) and MUST NOT reserve or charge until the Learner consents to that quote. A setting change alone MUST NOT charge. Consent MUST fail closed when the quote expired, no longer covers the characters, or the voice-config hash changed. Replaying or switching back to a stored voice MUST be free and emit `narration_cache_hit`.

#### Scenario: Decline
- **WHEN** a Plus Learner sees the quote for another voice and declines
- **THEN** nothing is reserved or charged, the setting is unchanged and the old narration replays

#### Scenario: Consent
- **WHEN** the Learner confirms the quote
- **THEN** the new voice is synthesized within that quote, the old voice and its asset are retained and only delivered characters are charged

#### Scenario: Not entitled
- **WHEN** a Learner without the benefit selects a curated voice
- **THEN** the API answers 403 `voice_not_entitled` without a quote or synthesis

### Requirement: Narration off skips work
When the Learner's narration setting or the server flag is off, the API MUST NOT enqueue narration, call the Engine, create quotes or charge. Pending rows MUST be parked as disabled without a failure event.

#### Scenario: Setting turned off mid-run
- **WHEN** the Learner turns narration off while a slide is pending
- **THEN** the slide becomes disabled (`narration_off`), no provider call is made and no Credits are spent

### Requirement: Per-voice narration rows
`slide_narrations` SHALL hold one row per lesson, slide and voice. Preference `narration_voice` MUST be a logical key, be part of dumps, exports and account deletion, and never hold vendor ids or prices.

#### Scenario: Restore
- **WHEN** a dump is restored
- **THEN** the preference, both voices' content hashes and asset reference counts match
