# Prompt routing spike (openspec change `prompt-skill-routing-and-guardrails`)

Throwaway, measured comparison of three `lesson_slides` prompts on live `tencent/hy4-flash` (effort `none`, `json_object`, `data_collection: deny`), plus a live router run on OpenRouter's Decisions API. Nothing here is a production path, nothing is wired into `mise run engine:eval`, `evals/promotion/` or `evals/datasets/`. Results and tables: [`RESULTS.md`](RESULTS.md). Decisions that use them: `openspec/changes/prompt-skill-routing-and-guardrails/design.md`.

## Arms

| Arm | Prompt |
|---|---|
| A_prod | The production lesson prompt (`lessontest::lesson_prompt`: head, `LESSON_SHAPE`, `regions::shape()` with the committed `regions.toml` limits), copied into `run.py`; context is a compact JSON of title, goal, level, locale |
| B_all_skills | A_prod plus all five skill bodies (what a monolithic prompt would carry if the skills were simply added) |
| C_routed | A_prod plus the skills chosen by the Decisions API router for that case (one `noul` question per skill, load at P(yes) at least the threshold) |

Skills: `diagram_layout`, `code_slide`, `math_notation`, `comparison_table`, `timeline` (61 to 78 tokens each, principle plus one compact example). 12 cases, hand-labelled with the expected skill set; no learner data.

## Scoring

Deterministic, no LLM judge: `valid` mirrors the lesson contract checks (JSON, 2 to 4 slides, root id, no script, URL, entity, backslash or `!important`, duration at least 400 ms per word and 3 to 60 s, test per taught topic, variant pool, 1 to 6 regions with known slot, role and description at most 300 characters, one slot per slide, `data-slot` present); `valid_ex_duration` drops the duration rule; `fit` is true when the lesson shows the feature the expected skill asks for (`<svg` and role diagram, `<pre>` or `<code>` and role code, Unicode math without LaTeX, `<table>`, `<ol>`). The heuristics are coarse; they measure whether the skill had an effect, not teaching quality (a judge and human review belong to the golden set work in tasks group 7).

## Run

```sh
cd evals/spikes/prompt-routing
# router-only: P(yes) per skill for several decision models (writes router-bench.json)
OPENROUTER_API_KEY=... python3 -I run.py --bench baseten/joey-1,liquid/d1.2,respan/span-02,baseten/bison-1,perplexity/pplx-decider-flash-v1.2,microsoft/microsoft-decision-1.1 --out router-bench.json
# 12 cases x 3 arms, stops at --budget (writes results.json)
ROUTER_MODEL=microsoft/microsoft-decision-1.1 ROUTER_THRESHOLD=0.9 OPENROUTER_API_KEY=... python3 -I run.py --out results.json --budget 0.20
```

The key comes from the environment only and is never printed or stored (the spike ran with `OPENROUTER_API_KEY_STAGING` from `~/.config/duolify/secrets.env` mapped for the command). Lesson text is not stored, only token counts, costs, latencies and check results. `results-run1.json` is an earlier full run with `baseten/joey-1` at 0.5, kept as a repeat for variance.
