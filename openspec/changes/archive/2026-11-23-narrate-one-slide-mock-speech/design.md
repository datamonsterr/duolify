## Decisions
- Logical voice key is `default` (ADR 0011 E12-02); the engine resolves it to a configured voice id. One default voice plus fixtures; live voices disabled (ADR 0012 E14-05).
- Dedup key: `sha256(text, voice_id, model_id, settings_fingerprint)`. Objects: `narration/v1/<hash>/audio.wav` and `alignment.json`. An existing object is reused and costs zero.
- Audio is reserved with its own `narration` quote (`NarrationCharacters` = text length) so a failure releases only audio; lesson content is never touched.
- The mock reports provider `elevenlabs` and the quoted audio model so settlement validates exactly as the live path will.
- Output validation (API and client): non-empty text; alignment covers every character, `start<=end`, non-decreasing, last end within tolerance of duration.
- Manifest slide gains `narration_status` from `slide_narrations` (absent row = `disabled`) and an optional `narration` object (`audio_url`, `alignment_url`, `duration_ms`). Audio is served only to the owner by an authenticated route.
- Retry: River with bounded attempts; terminal failure marks `failed` and releases the reservation.
