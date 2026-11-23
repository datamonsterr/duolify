# Reliable structured AI outputs on the production route

## Why
The live golden eval (PR #277) passed 12 to 15 of 20 cases on the production OpenRouter route (primary `tencent/hy4-flash`, fallback `z-ai/glm-5.3-flash`). Safety screens failed closed, but benign requests were blocked and must-block cases came back unavailable:
- Hy4 at reasoning `low` spent the 200 to 300 token screen cap on reasoning and returned `content: null` with `finish_reason: length`.
- GLM answered screens in prose (`**verdict:** pass`, `allowed: true`).
- The review screen asked for codes "from the course screening closed set" without listing it, so models invented `phishing` and `secrets`.
- Both models wrapped JSON in a ```` ```json ```` fence, which production did not accept.
- The second attempt repeated the same prompt, so it rarely fixed the first answer.
- Non-screen live cases sent the dataset text instead of the production prompts.

## What Changes
- Every JSON task sends an OpenRouter `response_format` (strict `json_schema` for screens, brief classification, note enrichment, preview and plain lesson generation; `json_object` for the other generation tasks) with `provider.require_parameters: true`. Both models list `response_format` and `structured_outputs` in their OpenRouter `supported_parameters`.
- One shared extraction rule (`engine_core::structured::extract_json`) for every JSON task: trim whitespace and strip one surrounding code fence, then strict validation. No prose parsing; unknown reason codes are rejected, not mapped.
- The bounded second attempt becomes a corrective repair: it replays the rejected answer and a user turn quoting the validation error. Attempt, deadline and USD caps from `bound-model-routing-failures` are unchanged; the repair turn is dropped when it would exceed the input envelope.
- Screens run Hy4 with reasoning `none` (ADR 0018), and every screen prompt spells out the closed enum with examples that include block cases.
- The lesson prompt moves into `engine-core` with its JSON shape spelled out, shared by the server and the eval. Plain lessons (no Character segments) request a strict lesson schema per call. Before acceptance, one deterministic fix-up (`lessontest::prepare_lesson`) touches only retry variants that reuse the source question's choices: a same-order copy is rotated with the existing `reorder_choices` rule, and a reordered copy gets `answer_index` pointing at the source's correct text. `validate_lesson` is unchanged and still rejects everything else; rejection reasons name the variant, the source choices and the offending slide text.
- Live eval cases for non-screen tasks run the production prompt builders and validators; the golden rows carry production request data and are re-pinned.
- Mock provider mode serves the same route identity as live (provider `openrouter` and the `models.toml` model ids per task), so the API's live quote runs end to end without paid calls; the quote guard stays strict in both modes. Fixture-only features key on `LlmProvider::is_fixture` instead of the provider name. Health appends one `route:<task>=…` entry per task after the two provider names (no proto change).

## Impact
Engine only (`engine-core`, `engine-providers`, `engine-server`, `engine-eval`), `engine/config/models.toml`, the golden dataset and its pin. No proto or API change.

## Data backup
No new stored data. The golden dataset revision is pinned by commit and SHA-256 in `evals/pinned-datasets.toml`, and the reviewed bytes stay in `evals/datasets/duolify-golden/golden.jsonl`, so the previous revision remains recoverable from Hugging Face history and git.

## Analytics
No new events. `$ai_generation` keeps `fallback_used` and the sanitized `error_code`; a rejected first answer still records `invalid_schema`, and the repair attempt is the existing second attempt.

## Model testing and reliability
- Unit tests: extraction (fenced, unfenced, prose, truncated, two fences), request construction per task and per model (schema, `require_parameters`, reasoning effort), validators with explained errors, and the corrective repair (quoted error, envelope limit, fail closed after two invalid answers).
- `mise run engine:eval` (offline) stays green.
- Live: `mise run engine:eval-golden` on the reviewed bytes with no judge, hard budget $1 for the change; acceptance is 20/20 on three consecutive full runs with no threshold lowered.
- Fallbacks stay bounded: at most two attempts, the same deadline split and USD ceiling.
