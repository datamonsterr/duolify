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

1. Set the task's `prompt_mode` back to `monolithic` and deploy. The monolithic prompt is byte-identical to the pre-routing one (tests `monolithic_lesson_prompt_is_rules_then_context` and `monolithic_assembly_returns_the_legacy_prompt_unchanged`), so no data migration is needed.
2. Cached router decisions are ignored in monolithic mode: the router is only consulted by the routed path. They expire by TTL (30 days); to drop them at once, bump `ROUTER_VERSION` in `engine-core/src/router/mod.rs` (part of the cache key).
3. To stop only the model router, the daily breaker closes it automatically past 1 percent of engine spend; a routed task whose router gives no answer uses its default skill set. There is no separate kill switch: flip the task to `monolithic`.
4. Input guard: unset `INPUT_GUARD_ENABLED` on the API and restart. Nothing else changes; the four existing screens keep running.

## Router and guard evidence (task 7.2)

Rows live in `evals/datasets/duolify-golden/pending-prompt-skills.jsonl` (not yet published to the Hugging Face dataset). Run by the controller with `OPENROUTER_API_KEY` in the environment, from `engine/`:

```sh
D=../evals/datasets/duolify-golden
# Router accuracy (40 rows, exact skill set, threshold 0.9) and the four router fallback rows; the model rows
# call the Decisions API only (no chat spend); the fallback rows are scripted and cost nothing.
cargo run -p engine-eval --bin duolify-eval -- --provider live --data $D/pending-prompt-skills.jsonl --limit 0 --budget 0.1 --reports ../evals/reports/router
# Fast guard (decision model) on the tutor and note rows; the default run uses the chat classifier.
GUARD_CLASSIFIER=fast cargo run -p engine-eval --bin duolify-eval -- --provider live --data $D/pending-prompt-skills.jsonl --limit 0 --budget 0.1 --reports ../evals/reports/guard-fast
```

Compare the two guard models by editing `GUARD_MODEL` in `engine-providers/src/decisions.rs` on a throwaway branch. Router thresholds are data: tune `--threshold` per skill on these rows, never on the 12 spike cases.

## Guard parity with the four screens (task 5.3)

The API keeps calling the four screens until the consolidated guard agrees with them on the same rows. From `engine/`, run by the controller (live, `OPENROUTER_API_KEY` set; roughly 9 screen calls and 9 guard calls, a few thousandths of a dollar):

```sh
D=../evals/datasets/duolify-golden
cargo run -p engine-eval --bin duolify-eval -- parity-rows $D/golden.jsonl /tmp/parity-rows.jsonl   # any scratch path
cargo run -p engine-eval --bin duolify-eval -- --provider live --data $D/golden.jsonl --limit 0 --budget 0.5 --reports ../evals/reports/parity-screens      # screens: use the report's steer, character, course and review rows
cargo run -p engine-eval --bin duolify-eval -- --provider live --data /tmp/parity-rows.jsonl --limit 0 --budget 0.5 --reports ../evals/reports/parity-guard
cargo run -p engine-eval --bin duolify-eval -- parity ../evals/reports/parity-screens/<report>.json ../evals/reports/parity-guard/<report>.json
```

`parity` lists every row where the screens and the guard differ and exits non-zero unless there is parity: the same rows on both sides and no row the screens pass that the guard fails. Only then switch the four screens (task 5.3), one surface per PR.
