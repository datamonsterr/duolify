## MODIFIED Requirements

### Requirement: Speech synthesis
The API SHALL request audio from the Engine through the existing `SpeechProvider` interface with ElevenLabs, Workers AI and a deterministic mock. `NARRATION_PROVIDER` MUST accept `workers_ai` or `elevenlabs` and default to `elevenlabs` pending owner approval. Opt-in Workers AI MUST use an approved `WORKERS_AI_TTS_MODEL` (`@cf/deepgram/aura-2-en`, `@cf/deepgram/aura-2-es`, or `@cf/deepgram/aura-1`) and a trusted model-specific voice. The Engine MUST skip Workers AI for a Course language its configured model does not support. Entitled curated voices and pinned Character dialogue SHALL use the approved ElevenLabs mappings and existing consent rules. The provider/model/voice settings and live gates MUST be resolved before any call. Vendor credentials MUST be secrets and excluded from logs and events. ElevenLabs MUST request character-level timing. When audio has no vendor timing, the provider adapter MUST derive a complete, monotonic character alignment from decoded audio duration and Unicode text.

#### Scenario: Audio with alignment
- **WHEN** the Engine synthesizes a Slide
- **THEN** the response has valid audio bytes, its measured duration and vendor or estimated character alignment covering every character

#### Scenario: Mock provider
- **WHEN** tests or draft CI run
- **THEN** the mock `SpeechProvider` returns fixed audio and alignment without network calls

#### Scenario: Unsupported language
- **WHEN** the selected Workers AI model supports English and the Course uses a different language
- **THEN** the Engine resolves the budgeted ElevenLabs candidate without attempting Workers AI

#### Scenario: Deprecated model configuration
- **WHEN** `WORKERS_AI_TTS_MODEL` names retired MeloTTS
- **THEN** configuration reports that the model is unavailable and no live call is made to it

### Requirement: Narration storage and deduplication
The API SHALL store audio and alignment JSON in versioned object storage with a key derived from text, provider, model, voice and settings. The API MUST look up each resolved candidate's hash before synthesis, reuse existing audio at zero Credits and deduplicate concurrent synthesis per hash. The successful asset MUST be associated with its actual provider and model. Existing references MUST remain usable.

#### Scenario: Identical narration
- **WHEN** two Slides or Learners need identical text with the same provider, model, voice and settings
- **THEN** the second request reuses the stored object without another synthesis call or charge

#### Scenario: Settings change
- **WHEN** the provider, voice or model in config changes
- **THEN** new synthesis uses a distinct hash and prior objects stay valid for replay and recovery

### Requirement: Narration failure fallback
With Workers AI selected, the system SHALL fall back to ElevenLabs within its own budget after an unavailable, ineligible or failed Workers AI attempt. When no eligible attempt can deliver audio the API MUST release the undelivered reservation, set `narration_status` to `failed` for terminal synthesis failure, and leave captions, the Slide and Lesson test available. The background job MUST use existing backoff for recoverable failures, and operators MAY request re-synthesis through the existing budget guard. Exhausted budgets MUST suppress additional calls for the blocked route.

#### Scenario: ElevenLabs down
- **WHEN** ElevenLabs is unreachable and no provider delivers audio within the allowed attempts
- **THEN** the Slide plays with captions, `narration_failed` is emitted and recoverable failure remains eligible for background retry

#### Scenario: Workers AI exhausted
- **WHEN** the Workers AI neuron budget cannot admit synthesis and ElevenLabs can
- **THEN** only the ElevenLabs route may synthesize after its own cache check and budget admission

### Requirement: Narration model testing and reliability
Narration text MUST have Golden set cases for `lesson_narration` with deterministic checks for non-empty, bounded, plain text in the correct language and an LLM judge meeting `evals/thresholds.toml`. Audio checks MUST cover decoded duration within Slide bounds, every character aligned, non-decreasing timestamps and alignment end within the existing audio-duration tolerance. Golden set cases MUST cover the supported Workers AI models, language gating, decode and timing failures, model-aware deduplication, budget exhaustion and the full fallback chain, with recorded fakes in draft CI and pinned live promotion evidence before serving Learner traffic. The existing timed, spend-bounded synthesis envelope SHALL cover every provider attempt.

#### Scenario: Alignment gap
- **WHEN** a synthesis result has alignment that does not cover the full text
- **THEN** the result is rejected, the eval check fails on such output and only an eligible bounded fallback or retry may run

#### Scenario: Timeout
- **WHEN** the vendor call times out and the allowed attempts cannot deliver audio
- **THEN** the work is cancelled within the deadline, the Slide falls back to captions and the reservation is released

### Requirement: Content-addressed narration assets
The API SHALL compute a versioned content hash from NFC and whitespace-normalized text, provider, model, resolved voice and Engine settings fingerprint, including the timing algorithm version for estimated alignment. Identical hashes MUST share audio and alignment and trigger at most one synthesis concurrently. The actual resolved hash MUST be stored on ready references. Existing ready audio MUST emit `narration_cache_hit` at zero Credits.

#### Scenario: Concurrent identical requests
- **WHEN** two Lessons with identical narration are synthesized at the same time
- **THEN** the provider is called once per distinct hash and both Lessons reference the same objects

#### Scenario: Settings change
- **WHEN** a voice setting or provider changes the fingerprint
- **THEN** a new asset key is created and the old version is retained for recovery

### Requirement: Bounded vendor synthesis
The Engine SHALL make live calls only with `NARRATION_LIVE_ENABLED=true`, the selected provider's credentials, an approved voice/model and applicable promotion evidence. Mock and disabled modes MUST stay offline. The combined vendor path MUST fit the existing configured deadline (at most 60 seconds) and two live attempts. Output MUST cover every character with non-decreasing timestamps ending within tolerance of decoded audio duration. Permanent rejection MUST stop retries on the rejecting route. Terminal failure MUST release undelivered audio Credits and leave captions available.

#### Scenario: Vendor outage
- **WHEN** the primary and the permitted retry or fallback are unavailable
- **THEN** the Slide is `failed` with captions and applicable recovery available, nothing is charged and telemetry carries only the bounded reason

#### Scenario: Permanent rejection
- **WHEN** the vendor answers 401, a permanent 422 or retired-model 410
- **THEN** no retry is made against that route

### Requirement: Monthly provider character budget
For ElevenLabs the API SHALL reserve worst-case characters for authorized attempts atomically in a durable append-only ledger shared across processes. Unknown submissions MUST count conservatively. The API MUST stop ElevenLabs admission above `NARRATION_MONTHLY_CHAR_LIMIT` times `NARRATION_BUDGET_STOP_PERCENT`. Reused audio MUST cost zero budget. Release uncalled reservations, reconcile recorded attempts and keep historical use through asset purges.

#### Scenario: Budget exhausted
- **WHEN** fresh narration would exceed the ElevenLabs stop line and no other eligible provider can deliver it
- **THEN** no new learner quote or provider reservation is made for an unadmitted call, captions remain and `narration_budget_exhausted` is emitted with a bounded diagnostic

#### Scenario: Reused audio
- **WHEN** a lesson reuses an existing asset
- **THEN** the cycle count does not change

#### Scenario: Operator regeneration
- **WHEN** the budget is exhausted
- **THEN** requested regenerations stay queued and make no provider request

#### Scenario: Concurrent admissions
- **WHEN** two processes request more than the remaining allowance together
- **THEN** atomic admissions prevent their combined reservations from exceeding the stop line

#### Scenario: Unknown outcome or asset purge
- **WHEN** a provider call has an unknown outcome or its asset is purged
- **THEN** its admitted characters remain counted conservatively in the cycle

#### Scenario: Refusal before any provider request
- **WHEN** the engine refuses a call as not promoted
- **THEN** the reservation is released and captions remain available if no eligible fallback exists

#### Scenario: Abandoned reservation
- **WHEN** a process dies after reserving and never settles
- **THEN** the full reservation stays counted for its cycle and cannot be reopened

## ADDED Requirements

### Requirement: Workers AI daily neuron budget
Live Workers AI synthesis MUST reserve the estimated worst-case neurons for each model attempt atomically in durable shared accounting bounded by the free daily account allocation. Use configured model-specific rates and conservative rounding. Admission MUST cover the UTC-day scope across processes and models, leaving configured headroom for other account usage. Uncalled reservations MUST release, unknown submitted outcomes MUST remain counted, and cache hits MUST consume no budget. Retain account aggregates, asset provenance and timing source in the existing Postgres backup and restore policy. Protect audio and alignment with versioned object backups and preserve prior references without paid recovery.

#### Scenario: Concurrent neuron exhaustion
- **WHEN** two call reservations individually fit the remaining free allocation but together exceed it
- **THEN** only an admitted request can reach Workers AI and the other takes the eligible fallback

#### Scenario: Restore assets across providers
- **WHEN** a restore contains two assets for the same text from different providers and older pre-change assets
- **THEN** all existing references resolve their original audio and alignment without new charges
