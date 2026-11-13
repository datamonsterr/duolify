## ADDED Requirements

### Requirement: Allowlisted Character voices and dialogue segments
Each narrator/dialogue segment SHALL identify a speaker key from the Lesson's pinned Cast. The API/Engine MUST resolve that Character's `voice_key` through a versioned ElevenLabs allowlist in trusted config, never through learner-authored provider IDs or voice-cloning instructions. New synthesis MUST use a configured default allowlisted voice when a selected voice is retired or unavailable. Speaker order and captions MUST remain aligned with segments. Existing ADR 0009 narration validation, mute, caption, credit and asynchronous delivery rules MUST remain enforced.

#### Scenario: Two Character voices
- **WHEN** a Slide alternates between guide narration and supporting dialogue
- **THEN** synthesis uses the respective allowlisted voices and the player preserves speaker order and captions

#### Scenario: Retired voice
- **WHEN** a pinned voice key no longer maps to an available approved voice
- **THEN** new synthesis resolves its configured safe default, records the actual voice and leaves existing cached audio unchanged

### Requirement: Voice-aware cache and unchanged narration pricing
Narration cache keys MUST continue to hash text, actual resolved voice, model and settings, including per-speaker segments. Identical requests MUST deduplicate; changing Character voice MUST affect new audio only. Cached narration MUST remain free and newly synthesized characters MUST use existing narration Pricing config and reservation/settlement rules, never an extra `character_create` charge. Failed synthesis MUST release its narration reservation and preserve caption-only playback.

#### Scenario: Same text in different voices
- **WHEN** two Characters speak the same text using different resolved voices
- **THEN** their cache keys differ and each new synthesis is billed only under existing narration rules

#### Scenario: Cache hit for a published Character
- **WHEN** a Course copy reuses stored dialogue audio
- **THEN** no ElevenLabs request or Character creation charge occurs

### Requirement: Character voice analytics events
The API SHALL emit PostHog `character_voice_fallback` (speaker_key, reason_code, voice_config_version) on default voice resolution and retain existing narration events with actual voice/model/character-count properties. Events MUST follow the `analytics` delta, respect opt-out and exclude spoken text, persona and voice credentials. Synthesis usage MUST continue to be reported under narration.

#### Scenario: Voice mapping fallback event
- **WHEN** a retired voice maps to the safe default for synthesis
- **THEN** `character_voice_fallback` reports only the bounded reason, speaker key and config version

### Requirement: Character narration data backup and retention
Speaker/segment order, actual voice/config references and alignment keys MUST live with manifests in Postgres and be covered by nightly dumps. Audio, alignment and required rig assets MUST live in versioned S3 storage and nightly protected asset copies/manifests, with restore checks under `custom-characters` and `data-backup`. Unshared data MUST be purged within 30 days of Lesson/account deletion; assets still referenced by Lessons or E15 snapshots MUST follow reference-counted retention.

#### Scenario: Restore segment voices
- **WHEN** Postgres and narration object backups are restored
- **THEN** the original segment order, voice references, audio and alignment resolve without paid synthesis

### Requirement: Character narration model testing and reliability
Golden set `character_persona_lesson` cases MUST check narration/dialogue persona and language; deterministic checks MUST require valid speaker/voice keys, speaking-rate bounds, alignment coverage and segment order with validity score 1.00. Scores MUST meet `evals/thresholds.toml`, including `custom-characters` adherence/injection gates, and pass `mise run engine:eval`. E14's 30 s synthesis timeout, at most two retries and per-Lesson spend cap MUST apply across all segments. There MUST be no new TTS provider or extra persona inference call for voice selection; exhausted ElevenLabs failures MUST fall back to captions only, with no failed-synthesis charge.

#### Scenario: Multi-speaker alignment failure
- **WHEN** a segment's alignment is missing or crosses its expected order
- **THEN** validation rejects it, bounded E14 retry applies and exhaustion leaves readable captions without blocking the Lesson test
