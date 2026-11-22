## Design

### Owner decision, 2026-11-23: route-specific economics

Derive the protected cost from the pricing version's own action route: use the higher of primary and fallback input rates and the higher of their output rates, multiply the configured input and output envelopes by those rates, then multiply by the configured attempt count and the existing 1.2066 protection factor. Preserve the existing protected-cost-per-Credit and paid-margin floors. A Sonnet-priced route must retain Sonnet-level cost protection; missing route or rate data must fail closed. This replaces stale static Sonnet-based action costs after the approved Hy4/GLM routing change.

Create an immutable approved successor to the active pricing version, retaining learner prices and all unrelated terms. Derive preview evaluation limits from the same approved configuration used for server quotes. The provider cost envelope must remain within the protected budget at current route rates. Record explicit length termination without logging model output.

Route transcription through OpenRouter with zero-data-retention and denied data collection, using qwen/qwen3-asr-0.6b then openai/whisper-large-v3-turbo. The future endpoint must enforce the cost and count caps and the client level gate before fallback.

Allocate holds by Credit source before applying the daily free USD cap; charge only the Daily free fraction, including on mixed holds. Persisted financial data continues through existing backups and restore drills; classify any new text columns. Transcription event properties contain duration bucket and model only, never audio or text. Reliability is measured with existing golden evaluations and the transcription spike; model failure uses the configured fallback.

### Owner decision, 2026-11-23: delegated product choices

Owner quote: "Skip apple dev account, voice using openrouter cheapest and good enough model, raise 2500 to 6000. You decide all". Preview output rises from 2,500 to 6,000 tokens at the existing Credit price; learner prices are unchanged. The Apple developer account is skipped (mobile is out of scope here).

Transcription model, chosen from the live OpenRouter catalog on 2026-11-23 (priced per second of audio, shown as $3.33 per million units, 3,330 nano-USD per second): `qwen/qwen3-asr-0.6b` is the primary. It ties the cheapest listed ASR options (`nvidia/nemotron-3.5-asr-streaming-multilingual-0.6b`, `openai/whisper-large-v3-turbo`) and costs less than half of `qwen/qwen3-asr-1.7b`, `openai/whisper-large-v3` ($7.5) and thirty times less than `openai/whisper-1` ($100). Quality: it supports 30 languages and 22 Chinese dialects, enough for short learner voice notes. `openai/whisper-large-v3-turbo` (same price, 99+ languages, widely proven) is the fallback, which covers languages the primary lacks. The bounded request ceiling and daily count cap limit exposure if the quality bar is not met.
