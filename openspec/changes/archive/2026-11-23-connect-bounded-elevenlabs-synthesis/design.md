## Decisions
- Provider lives in `engine-providers` (already depends on reqwest/wiremock); the trait gains `voice(key)` with a mock default so config-driven live voices need no new `Service` field.
- Output format defaults to `wav_16000` (exact WAV duration; matches the stored `audio.wav` key and the 1 MiB object cap, about 32 s). MP3 formats are supported by CBR estimate.
- `VendorFailure` classifies errors without carrying keys, bodies or text; `synthesize_bounded` stops on permanent failures and backs off before the single retry.
- Quote model must equal the configured model, else `failed_precondition` (fail closed).
- Retry-After is not honoured; the fixed backoff stays inside the quote deadline.
