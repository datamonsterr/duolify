## Design

### Owner decision, 2026-11-23: route-specific economics

Derive the protected cost from the pricing version's own action route: use the higher of primary and fallback input rates and the higher of their output rates, multiply the configured input and output envelopes by those rates, then multiply by the configured attempt count and the existing 1.2066 protection factor. Preserve the existing protected-cost-per-Credit and paid-margin floors. A Sonnet-priced route must retain Sonnet-level cost protection; missing route or rate data must fail closed. This replaces stale static Sonnet-based action costs after the approved Hy4/GLM routing change.

Create an immutable approved successor to the active pricing version, retaining learner prices and all unrelated terms. Derive preview evaluation limits from the same approved configuration used for server quotes. The provider cost envelope must remain within the protected budget at current route rates. Record explicit length termination without logging model output.

Route transcription through OpenRouter with zero-data-retention and denied data collection, using qwen/qwen3-asr-0.6b then openai/whisper-large-v3-turbo. The future endpoint must enforce the cost and count caps and the client level gate before fallback.

Allocate holds by Credit source before applying the daily free USD cap; charge only the Daily free fraction, including on mixed holds. Persisted financial data continues through existing backups and restore drills; classify any new text columns. Transcription event properties contain duration bucket and model only, never audio or text. Reliability is measured with existing golden evaluations and the transcription spike; model failure uses the configured fallback.
