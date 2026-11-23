# ADR 0025: Prompt skills, skill router and consolidated input guard

- Status: Accepted (self-harm wording and resources pending owner review, see Consequences)
- Date: 2026-11-23
- Change: `openspec/changes/prompt-skill-routing-and-guardrails`

## Context

Owner goal (2026-11-23): optimise prompt quality and cost by using a decision model to select which skills to load, guard inputs with a cheap model, and keep the main model free to produce the best output. A live spike (`evals/spikes/prompt-routing/RESULTS.md`) showed skill fit rising from 5/18 to 15/18 (routed) or 17/18 (all skills) with one example per skill, and the Decisions API routing in 0.7 s for under 0.00005 USD.

## Decision

The full table is `design.md` section 2 (D1 to D19). The load-bearing choices:

- Each AI task gets an always-on core plus optional **skills** in `engine/config/skills/<task>.toml`, fingerprinted in `promotion::SOURCES` (ADR 0019). A skill body is at most 150 tokens with one example.
- A **router** picks skills: deterministic rules, then Redis cache, then OpenRouter Decisions API behind a `SkillRouter` trait with a chat-model fallback, then a safe default set. It sees structured fields only, never free text, with `data_collection = deny`.
- One `input_guard` core plus surface skills replaces four separate screens. Creation and publication surfaces fail closed; tutor chat, notes and transcripts fail open after a deterministic pre-filter. New reason codes `self_harm` and `pii`.
- Prompts split into a byte-stable system prefix (core, skills) and a user part (delimited data), so provider prompt caching applies. `max_tokens` derives from the quote ceiling.
- `prompt_mode` per task stays `monolithic` until a live A/B passes the economics gate (ADR 0015, 0019). Router and guard are platform-paid, inside protected cost.

## Consequences

Lower input tokens than loading every skill, better skill fit than today, one policy for the guard. Costs: a second model call per lesson (cached 30 days), an alpha API dependency (mitigated by trait, fallback, default set), and more eval cases. The self-harm safe response ships with neutral placeholder copy and no named hotlines behind the rollout flag until the owner approves wording and per-locale resources.
