# ADR 0006: LLM providers, gated generation, golden set on Hugging Face

- Status: Proposed (confirm)
- Date: 2026-11-08

## Decision

- **Providers**: Claude API (Anthropic Messages API) is primary for course and lesson generation and tutor chat. OpenRouter (OpenAI-compatible API) is the fallback and the route for cheap tasks (note enrichment, classification). The engine has one `LlmProvider` trait with two implementations and a router with per-task model config, retries and fallback.
- **Model config**: model IDs live in `engine/config/models.toml`, never hard-coded in logic. Defaults: `claude-sonnet-5-5` for generation, `claude-haiku-5-5` for chat and notes.
- **Gated generation**: `GenerateCourseOutline` returns units and lesson titles only. `GenerateLesson` is called for lesson N+1 only after the API records a pass for lesson N. `PreviewCourse` returns sample slides before the learner commits.
- **Golden set**: Hugging Face dataset `datamonsterr/duolify-golden` (to be created by the owner). Each row: `task`, `input` (JSON), `rubric`, `checks` (deterministic assertions). The engine `eval` binary downloads a pinned dataset revision, runs cases, scores with deterministic checks plus an LLM judge, and fails under thresholds in `evals/thresholds.toml`.
- **CI**: draft PRs run the eval in mock mode (recorded fixtures, no API cost). Ready PRs touching `engine/` run live evals on a sample when `ANTHROPIC_API_KEY` and `HF_TOKEN` secrets exist.
- **Observability**: token usage and latency per call go to PostHog LLM analytics events (`$ai_generation`).

## Consequences

- Provider outages degrade to fallback, not failure.
- Prompt changes are measurable before merge.
