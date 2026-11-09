# ADR 0009: Slide narration with the ElevenLabs API

- Status: Accepted (owner decision, 2026-11-08)
- Date: 2026-11-08

## Context

Slides are short Motion HTML animations. The owner wants spoken narration for every slide. The proto `Slide` message already carries `narration` text.

## Decision

- The engine writes narration text per slide during lesson generation (it is part of the golden-set checks).
- The engine calls the ElevenLabs text-to-speech API through a `SpeechProvider` trait (one implementation now; a mock for tests). It requests audio with character-level timestamps so the player can sync slide checkpoints and captions.
- The API stores the audio and timestamp JSON in object storage next to the slide HTML, keyed by a hash of (text, voice, model, settings), so identical narration is never paid for twice.
- The client plays audio in the lesson player, shows captions, and keeps playback controls (play, pause, replay, speed) in sync with the slide animation. Narration can be muted; the lesson still works without audio.
- Voice and model IDs live in engine config. The API key (`ELEVENLABS_API_KEY`) is a secret.
- Narration cost is charged in credits per character as part of lesson generation (priced in epic E12).

## Consequences

- New contract fields (audio URL, alignment) and a narration RPC or event arrive with the narration OpenSpec change.
- Evals check narration text quality; audio is checked by duration and alignment presence, not by listening.
