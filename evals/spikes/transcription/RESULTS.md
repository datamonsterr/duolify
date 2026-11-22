# Transcription comparison results, 2026-11-22

Endpoint: `POST https://openrouter.ai/api/v1/audio/transcriptions`, JSON body with base64 `input_audio`,
`provider.data_collection = "deny"` on every request. Key: the `duolify-ci-gh` OpenRouter key.
Raw rows: [`results.json`](results.json) (transcripts, latency, cost per call); routing probe:
[`policy.json`](policy.json). Tables below are the output of `run.py report`.

Set: 16 clips, 119 s, 248 reference words (see `manifest.json`), each sent as WebM/Opus (libopus 24 kbps, 48 kHz)
and as MP4/AAC (`.m4a`, 32 kbps, 16 kHz), so 32 requests per model. One run, one machine, no retries; every
request succeeded (0 failures in 256). **The set is tiny: one word is 0.4 % of a model's total, so WER gaps under
about 2 points are noise.** Synthetic speech (the four technical clips) flatters every model.

## Word error rate (mean over clips, both containers; lower is better)

Method: lowercase, Unicode NFKC, punctuation and symbols removed, hyphens turned into spaces, apostrophes dropped,
spaces collapsed; word-level Levenshtein distance divided by reference words; clip WERs averaged per category
(`run.py: norm, wer`). Vietnamese is scored on space-separated syllables. Digits versus spelled numbers are
not normalised (none occur in the set).

| Model | accented en | clean en | noisy en | technical en | Vietnamese | all speech | failed calls |
|---|---|---|---|---|---|---|---|
| `openai/gpt-4o-mini-transcribe` | 1.8% | 1.3% | 1.9% | 1.6% | 13.1% | 4.1% | 0 |
| `openai/whisper-large-v3-turbo` | 0.0% | 3.1% | 1.9% | 1.6% | 6.1% | 2.7% | 0 |
| `google/gemini-3.5-transcribe` | 3.6% | 1.3% | 1.9% | 1.6% | 2.0% | 1.9% | 0 |
| `nvidia/nemotron-3.5-asr-streaming-multilingual-0.6b` | 4.2% | 5.4% | 3.2% | 4.7% | 9.3% | 5.6% | 0 |
| `qwen/qwen3-asr-0.6b` | 0.0% | 1.3% | 3.2% | 1.6% | 8.8% | 3.1% | 0 |
| `openai/gpt-4o-transcribe` | 0.0% | 2.5% | 0.0% | 1.6% | 6.6% | 2.4% | 0 |
| `mistralai/voxtral-mini-transcribe-3-0` | 0.0% | 3.1% | 1.9% | 1.6% | 3.1% | 2.1% | 0 |
| `nvidia/parakeet-tdt-0.6b-v3` | 0.0% | 2.9% | 4.4% | 1.6% | 100.0% | 23.1% | 0 |

The 1.6 % on technical clips is the same single word for every model (a spelling variant, not a term):
"const", "closure", "roadmap", "OKR(s)" came back right everywhere. Parakeet is an English and European-language model;
on Vietnamese it returned unrelated romanised text, so it is excluded from the language requirement.

## Container check

| Model | WER from WebM/Opus | WER from MP4/AAC |
|---|---|---|
| `openai/gpt-4o-mini-transcribe` | 4.0% | 4.2% |
| `openai/whisper-large-v3-turbo` | 3.1% | 2.3% |
| `google/gemini-3.5-transcribe` | 1.9% | 1.9% |
| `nvidia/nemotron-3.5-asr-streaming-multilingual-0.6b` | 5.4% | 5.7% |
| `qwen/qwen3-asr-0.6b` | 3.9% | 2.3% |
| `openai/gpt-4o-transcribe` | 1.6% | 3.1% |
| `mistralai/voxtral-mini-transcribe-3-0` | 2.2% | 1.9% |
| `nvidia/parakeet-tdt-0.6b-v3` | 23.1% | 23.1% |

All eight models accepted both containers (`format: "webm"` and `format: "m4a"`). Beyond the ffmpeg-made files, the
four models listed in the next paragraph also accepted real Chromium `MediaRecorder` output.

Real recordings (captured in Chromium 158 per `docs/design/voice-input-platforms.md`; 5 s, 30 s default bitrate
(483 KB), 30 s at 24 kbps, and a 10 s `audio/mp4` file that holds Opus): `gpt-4o-mini-transcribe`,
`whisper-large-v3-turbo`, `gemini-3.5-transcribe` and `qwen3-asr-0.6b` accepted all of them and returned the right text (the
30 s recording transcribed to 63 to 68 words of the 68-word source). Real Safari AAC output was not available.

## Silence, noise, latency, cost

Silence rule used: words returned after `norm()` for a 5 s digital-silence clip and a 6 s low-level pink-noise clip,
four requests per model (2 clips x 2 containers).

| Model | Words on silence / noise-only | Latency p50 / p95 (s), 32 clips | Cost per clip (USD), mean | Cost per 30 s of audio (USD) |
|---|---|---|---|---|
| `openai/gpt-4o-mini-transcribe` | 0 / 0 | 1.31 / 2.22 | 0.000200 | 0.00080 |
| `openai/whisper-large-v3-turbo` | **2 ("Thank you.") on silence**; "." on noise | 1.04 / 2.45 | 0.000083 | 0.00033 |
| `google/gemini-3.5-transcribe` | 0 / 0 | 4.00 / 4.36 | 0.000594 | 0.00239 |
| `nvidia/nemotron-3.5-asr-streaming-multilingual-0.6b` | 0 / 0 | 1.62 / 1.99 | 0.000025 | 0.00010 |
| `qwen/qwen3-asr-0.6b` | 0 / 0 | 1.18 / 1.87 | 0.000025 | 0.00010 |
| `openai/gpt-4o-transcribe` | **1 to 2 hallucinated words on all four** ("Blessing", "Selbstständiger.", "こんにちは。", "Mon travail.") | 1.44 / 3.49 | 0.000404 | 0.00163 |
| `mistralai/voxtral-mini-transcribe-3-0` | 0 / 0 | 1.18 / 2.71 | 0.000412 | 0.00166 |
| `nvidia/parakeet-tdt-0.6b-v3` | 0 / 0 | 1.17 / 2.69 | 0.000124 | 0.00050 |

- Latency is wall time of one HTTPS request from this machine (base64 upload included), clips 4 to 12 s.
  A single 30 s recording took 1.4 to 4.3 s for the four models tried, with one 11.7 s outlier for
  `qwen3-asr-0.6b` (n = 1 and not reproduced), so the budget below keeps headroom.
- Cost is `usage.cost` from the response. "Per 30 s" is total cost divided by total audio seconds, times 30.
  Whisper and the ASR models bill per second of audio; the OpenAI and Google models bill tokens.
- Prices listed by OpenRouter (USD per second of audio, `GET /api/v1/models?output_modalities=transcription`): whisper-large-v3-turbo
  3.33e-6 (DeepInfra) to 1.11e-5 (Groq, PrimeIntellect); the routed price depended on the provider chosen with
  data collection denied (observed 1.11e-5), so budget at the higher figure; qwen3-asr-0.6b 3.33e-6;
  voxtral-mini-transcribe-3-0 5.83e-5; gpt-4o-mini-transcribe 1.25e-6 per input token and 5e-6 per output token.

## Language

| Model | Detected language returned? |
|---|---|
| `openai/whisper-large-v3-turbo` | yes, `language` ("vi", "en") with `response_format: verbose_json` |
| `qwen/qwen3-asr-0.6b` | yes, `language` ("vi", "en") with `verbose_json` |
| `nvidia/nemotron-3.5-asr-streaming-multilingual-0.6b` | field present but the value is `"auto"` |
| `google/gemini-3.5-transcribe`, `mistralai/voxtral-mini-transcribe-3-0` | no language field (`segments`, `words` only) |
| `openai/gpt-4o-mini-transcribe`, `openai/gpt-4o-transcribe` | `verbose_json` rejected with 400; the default response has no language |

The Learner's course language is known to the app, so the language to log can come from the course or from a text
heuristic rather than from the model. Passing `language: "vi"` did not change the one weak gpt-4o-mini Vietnamese
clip, and `language: "en"` did not stop whisper's "Thank you." on silence.

## Data collection and zero data retention

Probe: the same clip with `provider.data_collection = "deny"` and with `provider.zdr = true` (`policy.json`).
OpenRouter's machine-readable list `GET /api/v1/endpoints/zdr` (fetched 2026-11-22) agrees.

| Model | `data_collection: deny` | `zdr: true` | ZDR endpoints (provider) |
|---|---|---|---|
| `openai/gpt-4o-mini-transcribe` | accepted | **404, no endpoints** | none |
| `openai/gpt-4o-transcribe` | accepted | **404, no endpoints** | none |
| `google/gemini-3.5-transcribe` | accepted | **404, no endpoints** | none |
| `openai/whisper-large-v3-turbo` | accepted | accepted | DeepInfra, Groq, PrimeIntellect |
| `qwen/qwen3-asr-0.6b` | accepted | accepted | DeepInfra, PrimeIntellect |
| `nvidia/nemotron-3.5-asr-streaming-multilingual-0.6b` | accepted | accepted | DeepInfra |
| `mistralai/voxtral-mini-transcribe-3-0` | accepted | accepted | Mistral |
| `nvidia/parakeet-tdt-0.6b-v3` | accepted | accepted | PrimeIntellect, Together |

"Accepted" for `deny` only means a routable endpoint exists under that preference; it does not prove what the
provider keeps. See ADR 0021 for the quoted provider policies.

## Spend

Own tally of `usage.cost`: full run 0.0597 + policy probe 0.0023 + real-recording check 0.0094 + language probe
about 0.002, about 0.074 USD of the 0.60 budget. (The CI key's daily usage read 0.20 USD at the end, so other
work shares that key; this spike did not spend that.) ElevenLabs: 346 characters of the 600 allowed.

## Recommendation

- **Primary: `mistralai/voxtral-mini-transcribe-3-0`.** WER 2.1% overall and 3.1% on Vietnamese (second best), nothing returned on
  silence or noise, p50 1.18 s / p95 2.71 s, about 0.0017 USD per 30 s, and a ZDR endpoint (Mistral). It is the only
  candidate that is accurate in both languages, clean on silence and zero-retention routable. Caveat: one provider
  endpoint only, so `zdr: true` leaves no alternate route inside the model.
- **Fallback: `qwen/qwen3-asr-0.6b`.** A different vendor and different providers (DeepInfra, PrimeIntellect, both ZDR), WER 3.1%,
  clean on silence, about 0.0001 USD per 30 s. Weakness: Vietnamese 8.8% and one 11.7 s latency outlier, so a tight timeout applies.
- **Not recommended.** `gpt-4o-mini-transcribe` and `gpt-4o-transcribe` (no ZDR endpoint through OpenRouter; gpt-4o-mini Vietnamese 13.1%;
  gpt-4o-transcribe hallucinates on silence). `gemini-3.5-transcribe` (best WER, but 4 s p50 and no ZDR endpoint).
  `whisper-large-v3-turbo` is the cheapest accurate option with ZDR but answers "Thank you." on digital silence, so it
  fails the silence rule unless the silence gate below sits in front of it. `parakeet-tdt-0.6b-v3` does not handle
  Vietnamese; `nemotron-3.5-asr` is the least accurate.

Proposed Golden-set thresholds (confirm on a larger set before promotion; this one has 248 reference words):

| Rule | Proposed value | Observed (primary / fallback) |
|---|---|---|
| WER ceiling, English categories (clean, accented, noisy, technical) | mean <= 6% and no clip above 20% | 1.7% / 1.5% |
| WER ceiling, Vietnamese | mean <= 12% | 3.1% / 8.8% |
| Silence and noise-only clips | 0 words after `norm()` (punctuation only counts as empty), for primary and fallback | 0 / 0 |
| Silence gate in the API or client (new rule) | do not upload a recording whose measured peak level is below a floor; treat a 3-word-or-shorter transcript of a quiet clip as empty | needed for any Whisper-family or OpenAI model |
| Latency per Engine attempt, 30 s clip | p50 <= 3 s, p95 <= 6 s; hard timeout 8 s per attempt, so primary plus one fallback finish within 16 s | p50 1.2 s, p95 2.7 s |
| Cost per request ceiling | 0.005 USD (30 s on the primary costs about 0.0017, 3x headroom) | 0.0017 / 0.0001 |
| Worst-case cost per Learner per day | 60 requests x 0.0017 = about 0.10 USD at the proposed cap, only if every request is a full 30 s | |
| Containers | WebM/Opus and MP4/AAC both accepted, each with the full clip set | all models passed both |
