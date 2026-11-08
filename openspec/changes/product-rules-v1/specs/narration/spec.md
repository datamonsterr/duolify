## ADDED Requirements

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
