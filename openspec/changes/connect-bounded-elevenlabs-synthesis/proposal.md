# Connect bounded ElevenLabs synthesis (E14-04)

## Why
E14-01..02 proved synthesis, pricing and recovery against a deterministic mock behind `SpeechProvider`. The real vendor must plug in behind the same contract, bounded by the quote, with captions as the only vendor-outage fallback.

## What Changes
- Engine: `ElevenLabsSpeech` calls `POST /v1/text-to-speech/{voice_id}/with-timestamps` (`xi-api-key`, `output_format`) and maps character alignment seconds to milliseconds. Output is validated for audio, every character, non-decreasing timestamps and duration derived from the audio itself.
- Voice, model and settings come from engine config (`ELEVENLABS_VOICE_ID`, `ELEVENLABS_MODEL_ID`, `ELEVENLABS_STABILITY`, `ELEVENLABS_SIMILARITY_BOOST`, `ELEVENLABS_SPEED`); the key comes only from `ELEVENLABS_API_KEY`. Live mode needs BOTH a key and `NARRATION_LIVE_ENABLED=true` plus a configured voice; otherwise the mock is used. Live voice enable stays gated by E14-05 / E10-06.
- Launch limits: 60 s per TTS call, one repair/retry after the primary within the quote, backoff before the retry. 401/422 are not retried; 429 exhausted maps to `resource_exhausted` (`rate_limited`), 5xx/timeouts to `unavailable`/`deadline_exceeded`.
- API: `rate_limited` failure code; terminal failure releases the undelivered reservation, the slide becomes captions-only (`failed`) and Retry stays available.
- Analytics: `narration_generated`/`narration_failed` and `$ai_generation` with character cost; never text.
- Tests: local fake HTTP server only (200, 429, 5xx, 401/422, malformed/decreasing alignment, timeout); recorded fixture `evals/fixtures/narration.vendor.json` and four golden rows. No test or CI run calls the live API.

## Impact
engine (core, providers, server), services/api narration + narrationtest twin, evals fixtures, `.env.example`, docs/data-inventory.md. No migration, no new persisted data.
