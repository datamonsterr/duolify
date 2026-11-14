## ADDED Requirements

### Requirement: Mock synthesis tracer
The Engine SHALL expose `SynthesizeNarration` through a `SpeechProvider` trait whose only implementation in this change is a deterministic mock. The response MUST carry the resolved voice id, model id and settings fingerprint, character count, audio bytes, duration, character alignment and actual usage. Live narration MUST default off and no network call is made.

#### Scenario: Deterministic mock
- **WHEN** the same text, voice key and settings are synthesized twice
- **THEN** both responses have identical bytes, duration and alignment

#### Scenario: Invalid alignment rejected
- **WHEN** alignment misses a character, goes backwards or ends far from the duration
- **THEN** the API rejects the output, marks the slide `failed` and releases only the audio reservation

### Requirement: Per-slide synthesis after stored text
The API SHALL queue synthesis for a slide only after its narration text is stored, reserve audio credits with a separate `narration` quote, store versioned audio and alignment objects, and set the manifest `narration_status` to `pending`, `ready`, `failed` or `disabled`. A Slide without text MUST be rejected. A failed synthesis MUST NOT block the text, the animation or the lesson test.

#### Scenario: Ready narration
- **WHEN** synthesis succeeds
- **THEN** the manifest status is `ready`, audio and alignment are served only to the owning Learner and `narration_generated` is emitted without text

#### Scenario: Cache hit
- **WHEN** the hash key already exists
- **THEN** no synthesis call is made and no Credits are spent
