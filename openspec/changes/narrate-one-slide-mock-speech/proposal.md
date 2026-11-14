# Synthesize and play one narrated slide (E14-01)

## Why
Slides carry narration text but no audio. This tracer proves the synthesis contract, character usage, object delivery and explicit Play with a deterministic mock before the real vendor (E14-04).

## What Changes
- Add `EngineService.SynthesizeNarration` (additive proto) with resolved voice/model/settings fingerprint, characters, bytes, duration, character alignment and actual usage; matching Go consumer and Rust producer.
- Add a `SpeechProvider` trait in the engine with a deterministic mock only. No ElevenLabs HTTP client; live narration stays off.
- API queues one River job per slide after its text is stored, reserves an audio-only `narration` quote separately, stores versioned audio and alignment objects under a hash key, and writes `pending|ready|failed|disabled` per slide into the manifest. Failure releases only that audio reservation; text and test stay usable.
- Client adds an explicit "Play narration" control (never autoplay) with alignment-synced captions and a captions-only recovery message.
- Analytics: `narration_generated`, `narration_failed`, `narration_played`, `$ai_generation` (feature `narration`); no learner text.
- Data: new `slide_narrations` table, object export/deletion/backup rules, inventory update. Golden-set `lesson_narration` mock fixtures.

## Impact
proto, services/api (narration, player, analytics, accounts privacy, backup fixtures), engine (core, server, eval fixtures), apps/client player, docs/data-inventory.md.
