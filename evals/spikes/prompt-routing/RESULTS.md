# Spike results (2026-11-23)

Model `tencent/hy4-flash`, effort none, `json_object`, 12 lesson cases, three prompt arms (see README). Two full runs; run 2 uses the best router from the router bench. Budget: 0.0105 + 0.0105 USD for generation and routing, plus 0.0009 USD for the router bench (cap was 0.30 USD).


## run 1 (router `baseten/joey-1`, threshold 0.5)

| Arm | Input tok | Cached input tok | Output tok | Cost USD (generation) | Cost incl. router | Latency p50 / max | Fully valid | Skill fit |
|---|---|---|---|---|---|---|---|---|
| A_prod | 587 | 0 | 2046 | 0.000308 | 0.000308 | 9.6 s / 13.6 s | 5/12 | 2/9 |
| B_all_skills | 935 | 0 | 1677 | 0.000268 | 0.000268 | 7.4 s / 10.6 s | 3/12 | 8/9 |
| C_routed | 703 | 85 | 1934 | 0.000293 | 0.000298 | 8.8 s / 14.2 s | 5/12 | 6/9 |

Router: p50 latency 0.70 s, cost 0.0000045 USD per request, exact skill set 4/12, 1.42 skills loaded per lesson on average, router errors 0.

## run 2 (router `microsoft/microsoft-decision-1.1`, threshold 0.9)

| Arm | Input tok | Cached input tok | Output tok | Cost USD (generation) | Cost incl. router | Latency p50 / max | Fully valid | Skill fit |
|---|---|---|---|---|---|---|---|---|
| A_prod | 587 | 491 | 1923 | 0.000274 | 0.000274 | 8.8 s / 15.4 s | 2/12 | 3/9 |
| B_all_skills | 935 | 720 | 1925 | 0.000279 | 0.000279 | 8.8 s / 12.3 s | 4/12 | 9/9 |
| C_routed | 660 | 277 | 2109 | 0.000310 | 0.000319 | 9.7 s / 11.5 s | 2/12 | 9/9 |

Router: p50 latency 0.72 s, cost 0.0000089 USD per request, exact skill set 11/12, 1.00 skills loaded per lesson on average, router errors 0.

## Both runs pooled (24 lessons per arm)

| Arm | Input tok | Output tok | Cost incl. router USD | Cost sd | Fully valid | Valid except duration rule (run 2 only) | Duration rule failed | Skill fit |
|---|---|---|---|---|---|---|---|---|
| A_prod | 587 | 1985 | 0.000291 | 0.000053 | 7/24 | 12/12 | 17/24 | 5/18 |
| B_all_skills | 935 | 1801 | 0.000274 | 0.000051 | 7/24 | 12/12 | 17/24 | 17/18 |
| C_routed | 682 | 2021 | 0.000308 | 0.000069 | 7/24 | 12/12 | 17/24 | 15/18 |

## Router bench (one `noul` question per skill, 12 cases, exact expected set)

| Model | Price per 1M input | 0.5 | 0.7 | 0.9 | Latency p50 | Cost per request |
|---|---|---|---|---|---|---|
| `baseten/joey-1` | 0.01 | 4/12 | 8/12 | 6/12 | 0.68 s | 0.0000045 |
| `liquid/d1.2` | 0.02 | 9/12 | 7/12 | 7/12 | 0.76 s | 0.0000047 |
| `respan/span-02` | 0.01 | 2/12 | 8/12 | 10/12 | 1.08 s | 0.0000071 |
| `baseten/bison-1` | 0.1 | 6/12 | 8/12 | 11/12 | 0.79 s | 0.0000453 |
| `perplexity/pplx-decider-flash-v1.2` | 0.01 | 1/12 | 9/12 | 7/12 | 0.78 s | 0.0000032 |
| `microsoft/microsoft-decision-1.1` | 0.042 | 5/12 | 8/12 | 11/12 | 0.73 s | 0.0000089 |

## Reading the numbers

- The only validity failure in run 2 is the `duration_ms >= 400 per narration word` rule: every other contract check passed on all 36 lessons. The rule fails in 17 of 24 attempts in every arm, so it is independent of the prompt variant. Production recovers with a repair turn (a second full call); a deterministic clamp in `prepare_lesson` removes that cost. Run 1 did not record the all-but-duration count; its full-validity rates (5, 3, 5 of 12) are consistent.
- Skill fit (a skill's feature appears in the lesson): A_prod 5/18, B_all_skills 17/18, C_routed 15/18 over the 18 cases that expect a skill (9 per run). The production prompt has no guidance for these lesson types, so the model rarely draws a diagram, table or timeline unprompted.
- Cost: pooled cost per lesson is 0.000291 (A), 0.000274 (B) and 0.000308 including the router (C). The spread between arms (about 6 percent) is smaller than the standard error of the means (sd 0.00005 to 0.00007 over 24 lessons, standard error about 0.000012), so the spike shows no cost difference at this skill-library size. Output tokens (about 2,000, 85 percent of cost) dominate and vary by 20 percent between runs of the same arm.
- Input: skills add 73 tokens (routed, run 2, 1.0 skills per lesson) to 348 tokens (all five) over the 587-token production prompt: routing carries about a fifth of the all-skills increase.
- Caching: Hy4 on OpenRouter cached input automatically with no `cache_control`. In run 2, 491 of 587 prompt tokens (84 percent) were cache reads for A_prod because every case shares one prefix; B_all_skills 77 percent; C_routed 42 percent, because routed prompts have more distinct prefixes. Cache hits bill at 0.0015 per 1M instead of 0.035, so a smaller set of distinct prefixes saves more than a shorter prompt. Run 1 ran cold (no provider cache yet), which shows the variance in hit rate between runs.
- Router: the Decisions API answered in 0.7 to 1.1 s p50 at 0.000003 to 0.000045 USD per request, and accepted `provider.data_collection = "deny"` on all six models (syntax accepted; enforcement is not documented). Accuracy depends on threshold: five of six models peak at 0.7 to 0.9, a 2B model at 0.5 fires on almost every skill (4/12). The 11/12 for `microsoft/microsoft-decision-1.1` at 0.9 was tuned on these same 12 cases and is an upper bound.
- Limits: 12 cases, one model, one sample per case and arm, a heuristic fit check and no teaching-quality judge. The spike supports the direction (routing keeps most of the fit at a fifth of the added tokens; the Decisions API is fast and cheap enough) and not a cost-saving claim.
