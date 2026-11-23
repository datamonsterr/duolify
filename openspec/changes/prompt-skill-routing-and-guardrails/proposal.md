## Why

Owner request (2026-11-23): optimise prompt quality and cost by using a small decision model ("Jev", OpenRouter Decisions API) to select which skills to load, add a cheap input guardrail, and keep the main model free to produce its best output.

Today every engine task sends one monolithic prompt as a single `user` message (`engine-providers` `request_model`). The rules come first and the learner data last, but nothing is optional: the same ~800 token lesson contract is sent for a Python closure lesson and for a Vietnamese-tones lesson, and no prompt carries guidance for diagrams, code, formulas, tables or timelines, so quality on those lesson types depends on the model's habits. The four safety screens (`steer_screen`, `character_screen`, `course_screen`, `screen_review`) are four prompts with four copies of the same injection and unsafe-content policy, and the tutor chat, notes, setup and brief chats have no input screen at all. Measured baseline and spike results are in `design.md`.

## What Changes

- New capability `prompt-skills`: each task prompt becomes a small always-on core (role, output contract, safety) plus optional versioned skill modules stored in `engine/config/skills/`, covered by the promotion fingerprint.
- A skill router that prefers deterministic rules, then a cached decision, then one call to a cheap decision model (OpenRouter Decisions API behind a trait, with a chat-model fallback), then a safe default set. It runs at most once per lesson or tutor thread.
- One `input_guard` pass for learner free text on a cheap model, reusing the existing screen verdict schema and replacing the four duplicated screen policies with one core plus per-surface skills. It adds `self_harm` (safe-response path, no model generation) and PII minimisation, with fail-closed or fail-open set per surface.
- Prompt layout for caching: system = core + skills in deterministic order; user = delimited learner data last. Dynamic `max_tokens` from the quote ceiling. Per-task model and reasoning overrides stay; a skill may request a higher reasoning effort within the ceiling.
- Evals: golden cases per skill, a router accuracy set, a guardrail set with a false-positive ceiling, and an A/B harness in `engine-eval` (monolithic versus routed). Rollout behind a per-task switch with promotion evidence.
- Analytics: `$ai_generation` gains `skills_loaded`, `router_source`, `guardrail_verdict` (ids and enums only, no content).
- Spike (this change, no production code): `evals/spikes/prompt-routing/`, 12 lesson cases times 3 prompt arms on live Hy4 Flash plus a live Decisions API router run.

No change to learner prices, `proto`, `services/api` or the client. No new data store: skills are config, router decisions are a cache with a TTL.

## Capabilities

### New Capabilities

- `prompt-skills`: skill modules, router, input guardrail, prompt layout and caching, flexibility rules, economics gates, evals and rollout.

### Modified Capabilities

- `ai-reliability`: A/B harness, skill and router and guardrail golden sets, fingerprint coverage, router and guardrail fallbacks.
- `analytics`: new `$ai_generation` properties.

## Impact

- Engine: `engine-core` (skill loader, router, guard, prompt assembly), `engine-providers` (system/user split, Decisions API client), `engine-eval` (A/B harness, new case kinds), `engine/config/skills/*.toml`, `models.toml` (`[routing]` and `[guard]` tables).
- API: only reads the new analytics properties already forwarded from engine usage; the guardrail replaces the API-side calls to the four screen RPCs without changing their contract in the first slice.
- Out of scope: `conversational-flows-and-mascot` (free-text intent routing will call this guard and router) and `custom-characters` (prompt to attributes will call this guard). Both are consumers; this change fixes the interfaces they use.
- ADRs: proposes an ADR (number taken when accepted) that records the decisions table in `design.md`.
- Backup and data: no new tables or buckets. Skills are files in the repo; the router decision cache is disposable (Redis, TTL) and excluded from backups.
