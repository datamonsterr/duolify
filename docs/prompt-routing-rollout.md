# Prompt routing rollout and rollback (ADR 0025)

Each prompt task has its own switch: `prompt_mode` in `engine/config/skills/<task>.toml`, `monolithic` (today's single prompt) or `routed` (core plus skills, system and user split). Merging code never changes a live prompt; a task changes only in its own PR that flips this one line.

## Promotion evidence for one task

Needs `OPENROUTER_API_KEY` in the environment (run by the controller, never by a worker). Same cases, two arms, one report each:

```sh
cd engine
D=../evals/datasets/duolify-golden
PROMPT_ARM=monolithic cargo run -p engine-eval --bin duolify-eval -- --provider live --data $D/pending-prompt-skills.jsonl --limit 0 --budget 0.5 --reports ../evals/reports/arm-monolithic
PROMPT_ARM=routed     cargo run -p engine-eval --bin duolify-eval -- --provider live --data $D/pending-prompt-skills.jsonl --limit 0 --budget 0.5 --reports ../evals/reports/arm-routed
mise run engine:compare -- ../evals/reports/arm-monolithic/<report>.json ../evals/reports/arm-routed/<report>.json
```

Run each arm twice and compare the paired cost difference with its standard error before trusting a small gap. The gate is the verdict line: routed passes at least as often and costs at most 3 percent more. `HOLD` means leave the task monolithic and record the numbers in `openspec/changes/prompt-skill-routing-and-guardrails/design.md`. On `PROMOTE`:

1. Flip `prompt_mode = "routed"` in the task's skills file, in its own PR.
2. Refresh the promotion evidence on that commit (`evals/README.md`, "Refreshing promotion evidence"), because the skills files are fingerprinted.
3. Commit the two arm reports' summary (numbers only) to the change's design notes.

## Rollback

1. Set the task's `prompt_mode` back to `monolithic` and deploy. The monolithic prompt is byte-identical to the pre-routing one (test `monolithic_prompts_are_byte_identical`), so no data migration is needed.
2. Cached router decisions are ignored in monolithic mode: the router is only consulted by the routed path. They expire by TTL (30 days); to drop them at once, bump `ROUTER_VERSION` in `engine-core/src/router/mod.rs` (part of the cache key).
3. To stop only the model router, unset `REDIS_URL` is not enough; remove the router install by running the engine in `mock` mode is not acceptable either. Use the breaker instead: the day's router budget closes automatically past 1 percent of engine spend, and a routed task with no router answer uses its default skill set.
4. Input guard: unset `INPUT_GUARD_ENABLED` on the API and restart. Nothing else changes; the four existing screens keep running.
