# ADR 0016: Structured outputs and no reasoning for safety screens

- Status: Proposed (engine agent, 2026-11-22; amends ADR 0015 "AI model routing")
- Date: 2026-11-22
- Change: `openspec/changes/reliable-structured-ai-outputs`

## Context

ADR 0015 runs the four safety screens (`steer_screen`, `character_screen`, `course_screen`, `screen_review`) on Hy4 Flash with reasoning `low`. The live golden eval of PR #277 showed this fails on the production route:

- The screen envelopes cap output at 200 to 300 tokens. On OpenRouter the output cap covers reasoning and visible output together ("On most providers, the request's `max_output_tokens` limit applies to reasoning and visible output combined", OpenRouter reasoning-tokens guide). Hy4 at `low` ended 9 of 33 screen calls with `finish_reason: length` and `content: null`, then fell back to GLM.
- GLM answered screens in prose instead of the two-field JSON.
- Raising the cap is not an option: the envelopes are part of the quoted, bounded routing (`bound-model-routing-failures`), and ADR 0015's own lesson sample shows `low` spending 1,247 reasoning tokens.

OpenRouter model metadata (`GET /api/v1/models`, 2026-11-22) lists `response_format` and `structured_outputs` in `supported_parameters` for both `tencent/hy4-flash` (one provider, Tencent) and `z-ai/glm-5.3-flash` (every listed endpoint). Hy4 reasoning is optional (`mandatory: false`); GLM reasoning is mandatory with efforts `max`, `high`, `low`.

## Decision

- Screens run Hy4 with reasoning `none`. GLM keeps `low` (it cannot disable reasoning); its screen answers used 10 to 60 reasoning tokens in probes, well inside the cap.
- Every screen request carries a strict `json_schema` with the closed reason-code enum, `provider.require_parameters: true`, and a prompt that spells out the enum with examples, including block cases. Other JSON tasks carry strict `json_schema` (brief classification, note enrichment, preview) or `json_object` (lesson, outline, outline changes, replan, character mapping).
- Safety judgment comes from the explicit policy, enum and examples, not from hidden reasoning. Screens stay fail closed.

## Consequences

- With reasoning `none`, Hy4 answered every screen case schema-valid on the first attempt in the live eval, at about 1.5 s and $0.00001 per screen.
- `provider.require_parameters` restricts routing to endpoints that honour `response_format`. Hy4 has a single provider today; if it ever drops the parameter, the call fails and the bounded fallback to GLM runs.
- The other tasks keep the efforts of ADR 0015.
