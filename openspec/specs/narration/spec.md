# narration Specification

## Purpose
Give Lessons spoken narration: text written during Lesson generation, synthesized per Slide, stored by content hash, priced and settled in Credits, and played with synchronized captions. Covers Character voices, vendor bounds and the monthly provider character budget.

## Requirements

### Requirement: Narration text
The Engine SHALL write narration text for every Slide during Lesson generation, in the Course language, as part of the `GenerateLesson` output. The text MUST fit the Slide: its length MUST be within the speaking-rate bounds in config for `duration_ms`, it MUST be plain text without markup, and it MUST agree with the Slide content. The API MUST reject a Slide without narration text and trigger regeneration.

#### Scenario: Narration present
- **WHEN** the Engine returns a Lesson
- **THEN** every Slide has non-empty narration text within the length bounds

#### Scenario: Missing narration
- **WHEN** a Slide arrives without narration text
- **THEN** the API rejects the Lesson and triggers retry or fallback

### Requirement: Speech synthesis
The API SHALL request audio from the Engine, and the Engine MUST synthesize it through a `SpeechProvider` trait with an ElevenLabs implementation and a mock for tests. The request MUST ask for character-level timestamps. Voice ID, model ID and settings MUST come from engine config. `ELEVENLABS_API_KEY` MUST be a secret in `.env.example` with a comment, and MUST NOT appear in logs or events. ElevenLabs API usage MUST follow current official docs, checked before coding.

#### Scenario: Audio with alignment
- **WHEN** the Engine synthesizes a Slide
- **THEN** the response has audio bytes, a duration and character alignment covering every character of the text

#### Scenario: Mock provider
- **WHEN** tests or draft CI run
- **THEN** the mock `SpeechProvider` returns fixed audio and alignment without network calls

### Requirement: Narration storage and deduplication
The API SHALL store audio and alignment JSON in object storage next to the Slide HTML, under a key derived from a hash of (text, voice, model, settings). Before any synthesis the API MUST look up the key. When the object exists it MUST be reused and the Learner MUST NOT be charged. Concurrent requests for the same key MUST result in one synthesis call.

#### Scenario: Identical narration
- **WHEN** two Slides, or two Learners, need the same text with the same voice, model and settings
- **THEN** the second request reuses the stored object, no ElevenLabs call is made and no Credits are spent

#### Scenario: Settings change
- **WHEN** the voice or model in config changes
- **THEN** the hash changes and new audio is synthesized for new Lessons, while old objects stay valid

### Requirement: Narration generation flow
Narration synthesis SHALL run asynchronously after the Lesson text is stored, per Slide, so that Slides are playable as soon as their HTML exists. The manifest MUST show each Slide's `narration_status` (`pending`, `ready`, `failed` or `disabled`), and the API MUST update it as each audio object completes. A Learner MAY turn narration off in settings, which sets `disabled` and skips synthesis and charges.

#### Scenario: Audio arrives later
- **WHEN** a Lesson is opened before all audio is ready
- **THEN** ready Slides play with audio and pending Slides play with captions, and the player picks up audio as the manifest updates

### Requirement: Narration credits
Narration cost MUST be charged in Credits per synthesized character as part of Lesson generation, using the reserve, settle and release flow of the `credits` spec. The per-character price is defined by the monetization change (E12) and read from config; this spec defines no price. A cache hit MUST cost zero. A failed synthesis MUST release its reservation and not charge. When Credits are insufficient for narration, the Lesson text MUST still be delivered with `narration_status` `disabled` and the Learner MUST see the upsell option.

#### Scenario: Settled by characters
- **WHEN** a Slide's narration is synthesized
- **THEN** a `spend` entry equals the characters synthesized times the configured price

#### Scenario: Failed synthesis
- **WHEN** synthesis fails
- **THEN** the reservation is released and no `spend` entry exists for it

#### Scenario: Not enough credits
- **WHEN** the Learner cannot cover narration
- **THEN** the Lesson plays with captions only and the upsell option is shown

### Requirement: Narration failure fallback
When synthesis fails after retries, the API MUST set `narration_status` to `failed`, the player MUST show captions only, and the Lesson MUST stay playable and testable. A failed Slide MUST be retried by a background job with backoff, and an operator MAY trigger a re-synthesis. A synthesis failure MUST NOT fail Lesson generation or block the next Lesson.

#### Scenario: ElevenLabs down
- **WHEN** ElevenLabs is unreachable
- **THEN** Slides play with captions, `narration_failed` is emitted, and the background job retries later

### Requirement: Narration analytics events
The system SHALL emit `narration_generated`, `narration_cache_hit`, `narration_failed` from the API and `narration_played`, `narration_muted`, `narration_speed_changed` and `captions_toggled` from the client, with properties from the `analytics` spec. The Engine usage for synthesis MUST be reported with `feature` `narration` and the number of characters. Narration text MUST NOT be sent to PostHog.

#### Scenario: Generated
- **WHEN** synthesis succeeds
- **THEN** `narration_generated` is emitted with `characters`, `voice_id`, `model` and `latency_ms`

### Requirement: Narration data backup and retention
Narration text MUST be stored with the Slide manifest in Postgres. Audio and alignment objects MUST be stored in object storage with versioning enabled and included in the backup policy of the `data-backup` spec, because re-synthesis costs Credits and provider spend. Device caches are disposable. Hash-keyed objects shared between Lessons MUST be deleted only when no Lesson references them. Account deletion MUST remove Lessons and their unshared narration objects within 30 days.

#### Scenario: Audio object lost
- **WHEN** an audio object is missing in storage
- **THEN** it is restored from the previous version or re-synthesized by an operator job, and the Learner is not charged again

#### Scenario: Account deleted
- **WHEN** a Learner deletes their account
- **THEN** their narration text is deleted and objects referenced by no other Lesson are removed within 30 days

### Requirement: Narration model testing and reliability
Narration text MUST have Golden set cases (task `lesson_narration`) with deterministic checks: non-empty, length within bounds for the Slide duration, plain text, correct language, no unspoken markup. Audio MUST be checked by deterministic checks, not by listening: duration present and within bounds of the Slide, alignment present and covering every character, timestamps non-decreasing, alignment end within a tolerance of the audio duration. An LLM judge MUST score narration against the rubric and meet `evals/thresholds.toml`. Draft CI MUST use the mock `SpeechProvider`. The Engine MUST apply a per-call timeout (default 30 s, config), at most 2 retries with backoff, and a spend cap per Lesson from config. There is no second TTS provider in this version; the fallback is captions only.

#### Scenario: Alignment gap
- **WHEN** a synthesis result has alignment that does not cover the full text
- **THEN** the result is rejected, retried, and the eval check fails on such output

#### Scenario: Timeout
- **WHEN** a call exceeds the timeout
- **THEN** it is cancelled, retried up to the limit, then marked `failed` with the reservation released

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

### Requirement: Content-addressed narration assets
The API SHALL compute a narration content hash from normalized text (Unicode NFC, collapsed whitespace), voice id, model id and the Engine's resolved settings fingerprint before any provider call. Identical hashes MUST share one stored audio and alignment object and MUST trigger at most one synthesis, including under concurrent requests. An existing ready hash MUST be served as `narration_cache_hit` at zero Credits.

#### Scenario: Concurrent identical requests
- **WHEN** two Lessons with identical narration are synthesized at the same time
- **THEN** the provider is called once per distinct hash and both Lessons reference the same objects

#### Scenario: Settings change
- **WHEN** a voice setting changes the fingerprint
- **THEN** a new asset version is created and the old version is retained for recovery

### Requirement: Reference-counted retention
Each asset SHALL carry a reference count of ready slides. An asset and its objects MUST be deleted only after the last reference ends and the configured owner retention period (`NARRATION_RETENTION_DAYS`, default 30) elapses. Backups MUST contain hashes, object keys and reference counts.

#### Scenario: Shared asset survives one deletion
- **WHEN** one of two Lessons sharing an asset is deleted
- **THEN** the objects stay and the count drops to one

#### Scenario: Last reference
- **WHEN** the last reference is deleted
- **THEN** the objects are kept until retention elapses and are then deleted

### Requirement: Narration recovery without charge
The Learner MAY retry failed audio and restore a missing object. Retry MUST re-queue the same content hash. A missing object MUST be restored from the latest retained bucket version, else from a retained prior settings version of the same text, else through a tracked operator regeneration. None of these actions MAY reserve or charge Credits.

#### Scenario: Missing object, no prior version
- **WHEN** an object is missing and nothing can be restored
- **THEN** one regeneration is tracked per hash, the slide shows captions and "being restored", and completion needs no Learner payment

### Requirement: Bounded vendor synthesis
The Engine SHALL synthesize narration through ElevenLabs only when `ELEVENLABS_API_KEY` is set AND `NARRATION_LIVE_ENABLED=true` with a configured voice; otherwise it MUST use the mock. Each call SHALL be limited to 60 seconds and one repair/retry after the primary within the quote caps. Output MUST cover every character with non-decreasing timestamps ending near the audio duration, else it is rejected. A terminal vendor failure MUST release the undelivered reservation, charge nothing and leave captions as the only fallback.

#### Scenario: Vendor outage
- **WHEN** the vendor answers 503 on the primary and the retry
- **THEN** the slide is `failed` with captions available, Retry is offered, nothing is charged and no narration text appears in events or errors

#### Scenario: Permanent rejection
- **WHEN** the vendor answers 401 or 422
- **THEN** no retry is made

### Requirement: Synchronized narration clock and offline caching
Narration audio and alignment data SHALL integrate with the player's master clock. Audio playback rate MUST follow the user-selected playback speed (0.75, 1, 1.25, 1.5). When a lesson is opened, narration audio and alignment for upcoming slides MUST be preloaded and cached in client storage. Offline restart MUST utilize cached narration assets when present, and fall back to caption display when absent without charging credits.

#### Scenario: Preload next slide narration
- **WHEN** slide N is playing
- **THEN** audio and alignment for slide N+1 are preloaded into client cache

#### Scenario: Speed control affects narration
- **WHEN** the Learner changes playback speed to 1.25x
- **THEN** audio playback rate and slide animation rate update simultaneously

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

### Requirement: Monthly provider character budget
When live synthesis is enabled the API SHALL count the characters sent to the narration provider in the current subscription cycle from stored narration assets and completed regenerations, and MUST NOT make a live request that would take the cycle total past the configured stop line (`NARRATION_MONTHLY_CHAR_LIMIT` times `NARRATION_BUDGET_STOP_PERCENT`, defaults 40000 and 95%). Reused or shared audio MUST NOT count.

#### Scenario: Budget exhausted
- **WHEN** a lesson's fresh narration would exceed the stop line
- **THEN** no quote, reservation or provider request is made, the slides show captions only, `narration_budget_exhausted` is emitted and an error log line is written

#### Scenario: Reused audio
- **WHEN** a lesson reuses an existing asset
- **THEN** the cycle count does not change

#### Scenario: Operator regeneration
- **WHEN** the budget is exhausted
- **THEN** requested regenerations stay queued and make no provider request
