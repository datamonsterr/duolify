# ADR 0021: Voice input data handling

- Status: Proposed (voice-input spike, 2026-11-22; for owner review)
- Date: 2026-11-22
- Change: `openspec/changes/immersive-mobile-lesson-player` (tasks 1.2 to 1.4, 6.x)
- Evidence: `evals/spikes/transcription/RESULTS.md`, `docs/design/voice-input-platforms.md`
- Numbering: 0020 is reserved for daily check-in credits; 0021 was the next free number.

## Context

The Learner can dictate a Tutor chat question or a Quick note by holding a microphone control. Audio is personal data
that can carry a voice, a name or a room. ADR 0015 routes all AI through OpenRouter with `provider.data_collection = "deny"`
and no Claude models. OpenRouter's audio transcription endpoint (`/api/v1/audio/transcriptions`) forwards the audio to
a third-party provider that OpenRouter selects. The spike measured eight models: which are accurate and clean on silence,
and which can be routed with zero data retention (ZDR).

## Decision

1. **Capture.** The client records only while the Learner holds the control or between two taps, with a visible
   indicator, level meter and timer, and stops every microphone track at the end. Slide-off or Escape discards it.
2. **Path.** Audio goes from the client to `POST /v1/voice/transcriptions` over TLS, from the API to the Engine's
   `Transcribe` RPC, and from the Engine to OpenRouter, all as in-memory bytes for the length of the request.
3. **Never stored.** Audio is never written to disk, object storage, a queue, a log, an analytics event, a trace or a backup, by
   the client, API, Engine, proxy or any worker. Request bodies for this route are excluded from access logging and error
   reporting. Only the transcript persists, as the Tutor chat message or Quick note it becomes, under their existing
   retention and erasure rules. A failed or empty transcription creates nothing.
4. **Provider routing.** Every transcription request sends `provider.data_collection = "deny"` and, for the models below,
   `provider.zdr = true`. The Engine fails the request (typed error, no silent downgrade) rather than send audio to a route that does not
   satisfy both. Models without a ZDR endpoint are not used for audio.
5. **Models.** Primary `mistralai/voxtral-mini-transcribe-3-0`, fallback `qwen/qwen3-asr-0.6b`, as the `transcribe` task in
   `engine/config/models.toml`, subject to promotion evidence (ADR 0019). Both have OpenRouter ZDR endpoints (Mistral;
   DeepInfra and PrimeIntellect) and returned nothing on silence. Thresholds are in `RESULTS.md`.
6. **Limits.** 30 seconds, 1 MB (the client requests 32 kbps; Chromium's default of about 129 kbps would use 483 KB), 0.5 s minimum,
   a per-Learner rate limit and a daily cap (proposed 60), enforced in the API before any Engine call; containers WebM/Opus and
   MP4/AAC. A level check keeps silent recordings from being uploaded.
7. **What is logged.** Per request: Learner id, purpose (`tutor` or `note`), duration bucket (for example `<2s`, `2-5s`, `5-15s`, `15-30s`),
   language, model id, outcome, error class, latency and cost. Never the transcript text, never audio, never audio bytes or
   hashes. Analytics events follow the same rule (closed enums and ids only).

## Consequences

- **What the providers may retain.** Our own systems keep no audio, but a provider is a separate party, and OpenRouter cannot make a
  provider's policy for us. What is published (read 2026-11-22):
  - OpenRouter: "We do not persist image, audio or video files beyond the duration necessary to route the request" and "OpenRouter does not use
    your Inputs or Outputs for model training"; model providers "may, depending on their own terms and data practices, retain and use your Inputs
    and Outputs" ([privacy policy](https://openrouter.ai/privacy)). ZDR "means no routine storage of prompts or responses after processing"
    ([ZDR guide](https://openrouter.ai/docs/guides/features/zdr)); eligible endpoints are listed at `GET /api/v1/endpoints/zdr`.
  - **Mistral (primary):** OpenRouter lists a ZDR endpoint for `voxtral-mini-transcribe-3-0`. We could not retrieve Mistral's own published retention
    text for its API (the help-centre pages fetched did not state it), so the guarantee rests on OpenRouter's ZDR flag. Mistral's own terms still need
    reading by the owner before launch.
  - **DeepInfra (fallback `qwen3-asr-0.6b`):** "we do not store the data you submit to our APIs on disk. ... We only hold it in memory during
    the inference process" and "We do not log the content of your requests" ([data privacy](https://docs.deepinfra.com/account/data-privacy)). PrimeIntellect, the
    other fallback endpoint, is ZDR-listed by OpenRouter; its own text was not read.
  - **Not chosen, for the record:** OpenAI states that abuse-monitoring logs are "retained for up to 30 days" for API usage, with `/v1/audio/transcriptions` listed
    as having none and as ZDR-eligible after approval ([your data](https://developers.openai.com/api/docs/guides/your-data)), but OpenRouter lists **no ZDR endpoint**
    for `gpt-4o-mini-transcribe` or `gpt-4o-transcribe`, and Google's paid terms say "Google logs prompts and responses for a limited period of time" for abuse detection
    with no stated duration ([Gemini API terms](https://aistudio.google.com/docs/terms)); `gemini-3.5-transcribe` also has no ZDR endpoint. Where denial of
    retention cannot be guaranteed, that weighed against the model in the recommendation, even though gpt-4o-mini-transcribe and Gemini are accurate.
  - `data_collection: "deny"` filters out providers that train on or collect prompts; it is a routing preference and does not by itself prove zero retention.
    ZDR is the stronger flag and is why it is required in decision 4.
- Requiring both flags narrows the route: `voxtral-mini-transcribe-3-0` has a single endpoint, so an outage at Mistral sends traffic to the fallback; if both are
  down, voice input is unavailable and typing still works.
- The privacy claim in the spec ("never stored") holds for Duolify systems; the user-facing wording must say audio is processed by a speech
  provider that does not keep it, not that no third party sees it.
- The microphone permission strings are an App Store and Play review surface; they say the microphone is used only while the control is held.
- Backups, the data inventory and the erasure flow need no audio handling; `docs/data-inventory.md` records "no audio retention" (task 7.1).
- Voice quality varies by language (Vietnamese WER 3.1% to 8.8% for the pair, against 1.5% to 1.7% for English categories), which is why the transcript is
  shown and editable before it is used.

## Alternatives considered

- **On-device recognition (Web Speech API).** `SpeechRecognition` is not Baseline; MDN says some browsers use a server-based engine by default and send audio to a web
  service, and on-device use needs `processLocally` plus a downloaded language pack. MDN does not cover WKWebView or Android WebView. Availability there was not verified, and
  Android WebView is generally reported not to implement `SpeechRecognition` (unverified). It would also give different behaviour per platform and no control of the language
  set (Vietnamese). Rejected for the first release; may return as an offline fallback.
- **Store audio for replay.** Not needed by any requirement, would make recordings personal data with retention, export, erasure and backup duties, and enlarges the breach
  surface. Rejected.
- **A dedicated speech vendor outside OpenRouter** (for example Deepgram or AssemblyAI directly). Would break ADR 0015's single provider route, add a key and a vendor review. Models
  from several of these vendors are available on OpenRouter if needed later.
- **Whisper-family primary.** Cheapest, ZDR-routable, but returned "Thank you." on silence; acceptable only behind a level gate. Kept as a candidate for a larger Golden set.
