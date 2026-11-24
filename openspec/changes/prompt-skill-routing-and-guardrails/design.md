## Context

Owner request, 2026-11-23: use a small decision model to choose which skills to load into a prompt (the owner's coding setup does this with "Jev"), guard inputs with a cheap model, and keep the main model flexible. The owner delegated product decisions; every decision below is made and justified, and only true owner-only items are left in "Owner questions".

"Jev" is TypeSafe's decision model on OpenRouter's Decisions API (`POST /api/alpha/decisions`). It does not generate text: it answers typed questions (`choice`, `noul`, `score`, `box`) about a `state` with a probability, and bills input tokens only. Docs checked 2026-11-23: `openrouter.ai/docs/guides/features/decisions`, `.../community/jev`, model list `output_modalities=decisions`.

## 1. Baseline (measured)

Sources: `evals/promotion/live-report.json` (90 live cases, budget 0.50 USD, spent 0.0070 USD, all 90 passed, `prompt_revision` git-head), prompt text in `engine/crates/engine-core`, `engine/config/models.toml`. Sample sizes are small (1 to 7 cases per task, except transcribe 49), so means are indicative; the spike below adds a larger live sample for `lesson_slides`.

| Task (route) | n | Input tokens | Output tokens | Cost per call (USD) | Latency p50 | Pass | Fallback used |
|---|---|---|---|---|---|---|---|
| lesson_slides (lesson) | 7 | 865 | 1,961 | 0.000342 | 10.5 s (max 22.1 s) | 7/7 | 1 |
| course_preview | 1 | 219 | 4,938 | 0.000704 | 26.3 s | 1/1 | 0 |
| outline_replan | 3 | 308 | 1,461 | 0.000217 | 12.8 s | 3/3 | 0 |
| course_outline | 1 | 322 | 824 | 0.000127 | 6.6 s | 1/1 | 0 |
| character_create | 3 | 247 | 120 | 0.000244 | 17.3 s | 3/3 | 2 |
| lesson_test | 1 | 225 | 292 | 0.000049 | 3.5 s | 1/1 | 0 |
| tutor_chat | 7 | 1,005 | 71 | 0.000045 | 2.0 s | 7/7 | 0 |
| note_enrich | 1 | 186 | 61 | 0.000015 | 2.4 s | 1/1 | 0 |
| outline_chat_change | 3 | 236 | 25 | 0.000012 | 1.6 s | 3/3 | 0 |
| course_brief_chat | 1 | 191 | 8 | 0.000008 | 2.2 s | 1/1 | 0 |
| steer_screen | 3 | 350 | 12 | 0.000014 | 1.5 s | 3/3 | 0 |
| character_screen | 2 | 342 | 11 | 0.000014 | 3.5 s | 2/2 | 0 |
| course_screen | 2 | 474 | 19 | 0.000019 | 1.7 s | 2/2 | 0 |
| screen_review | 2 | 411 | 16 | 0.000017 | 1.8 s | 2/2 | 0 |

Prices (ADR 0015, OpenRouter, 2026-11-22; re-checked 2026-11-23): Hy4 Flash 0.035 input, 0.1405 output, 0.0015 cache read per 1M tokens. Reasoning tokens bill as output.

What the numbers say:

1. Output dominates cost. `lesson_slides`: 1,961 output tokens cost 0.000276 USD, 865 input tokens cost 0.000030 USD. Output is about 81 percent of a lesson and input about 9 percent. `course_preview` is output-bound even more (4,938 tokens). Trimming instructions cannot move cost much; steering output length and avoiding repairs and fallbacks can.
2. Cache read is 23 times cheaper than input (0.0015 versus 0.035). The cheapest lever on input-heavy tasks is a byte-stable prefix, not a shorter prompt. `tutor_chat` is input-bound (1,005 in, 71 out): a cached 430-token instruction prefix saves about 0.0000144 USD per message, about 32 percent of the call.
3. Per-learner-day spend is roughly equal between lessons and chat: one lesson 0.00034 USD; eight chat messages (the daily free allowance, `CONTEXT.md`) about 0.00036 USD. Both are far under the protected cost ceiling of 0.0015 USD per credit (`economics.go`, `minimum_paid_ai_margin_percent` 70), so this change improves margin headroom and quality; it is not needed to meet the margin floor.
4. Fallback and latency are where reliability is lost: `character_create` fell back in 2 of 3 cases (17.3 s), `lesson_slides` once in 7 (max 22 s). Each fallback or repair doubles the call.

### Where prompts carry instructions most requests do not need

Sizes are tokens at the Hy4 tokenizer ratio measured in the spike (0.2445 tokens per character).

| Task | Instruction tokens | Part that applies to a minority of requests |
|---|---|---|
| tutor_chat (`chat::INSTRUCTIONS`) | about 430 | `selected_region` and `whole_slide` paragraph about 132 (only when the learner selected a region); persona paragraph about 77 (only with a persona); both are 48 percent of the instructions |
| lesson_slides (`LESSON_SHAPE` + regions + head) | about 548 | none is optional today (regions are player-required, 312). The gap is the opposite: there is no guidance for diagrams, code, formulas, tables, timelines, languages or age band, and per-request add-ons (cast segments, steering) are already appended conditionally in `engine-server` |
| lesson_slides context | not measured | `format!("{context:?}")` renders the gRPC request with Rust `Debug` (field names, `Some(..)`, empty fields), unmeasured but certainly verbose |
| steer_screen / character_screen / course_screen / screen_review | 100 to 230 each | four copies of the same injection and unsafe-content policy with four example sets and two verdict shapes (`allowed` versus `approved`) |
| note_enrich, outline_chat_change, course_preview, course_outline | 70 to 170 | mostly each task's contract; the shared "learner fields are untrusted data" sentence is copied in all of them |

Other findings:

- All prompts are one `user` message (`engine-providers::request_model`); only course steering lifts a fixed policy to `system`. Layout is already "rules first, data last", so prefix caching can work but is never verified: usage has `cache_read_nanos` pricing yet no eval or event records cached tokens.
- Guardrails: the four screens exist; tutor chat, notes, setup chat, brief chat and transcripts have no input screen, and nothing screens for self-harm or PII.
- The `duration_ms >= 400 per narration word` rule is the only contract rule Hy4 at effort none breaks in the spike: it fails in 17 of 24 first attempts in every prompt variant (all other checks passed on all 36 lessons of run 2). Production pays a repair call for it. It is a deterministic fix-up candidate (clamp in `prepare_lesson`), not prompt prose.

## 2. Decisions

| # | Decision | Choice | Why |
|---|---|---|---|
| D1 | Prompt structure | Always-on core plus optional skill modules, per task | The core stays validator-bound; guidance for diagrams, code and others gets added only when relevant. Spike (24 lessons per arm): skill fit 5/18 with the production prompt, 17/18 with all five skills (+348 input tokens, +59 percent), 15/18 routed (+73 to +116 tokens, a fifth to a third of the increase) |
| D2 | Skill storage | `engine/config/skills/<task>.toml`, listed in `promotion::SOURCES` | Same pattern as `models.toml` and `regions.toml`; fingerprint makes evidence stale on any change (ADR 0019); no DB, no deploy-time data |
| D3 | Router source order | Rules, then cache, then Decisions API model, then safe default set | Most skills follow from fields the engine already holds (cast, region, steering, locale, age band, course flags). Only content kinds need a model. The existing `characters` and `steering` code already assembles prompts conditionally, which is rules routing in all but name |
| D4 | Router model | OpenRouter Decisions API behind a `SkillRouter` trait, one `noul` per undecided skill; chat-model enum fallback; default set on failure | Decision models answered in 0.7 to 1.1 s for 0.000003 to 0.000045 USD per request with probabilities (spike, six models). The API is marked alpha, so it sits behind a trait with a fallback. Model chosen by the router golden set; the spike favours `microsoft/microsoft-decision-1.1` at threshold 0.9 (11 of 12 exact, 0.0000089 USD, 0.73 s) over `baseten/joey-1` (best 8 of 12 at 0.7) and `liquid/d1.2` (best 9 of 12 at 0.5); `baseten/bison-1` ties at 11 of 12 but costs five times more. The choice is tuned on the spike's 12 cases and is re-decided on the router golden set |
| D5 | Router run frequency | Once per lesson generation and once per tutor thread plus lesson; cached 30 days; never on retry or repair | A lesson is generated once (gated generation), so per-lesson equals per-course-lesson; routing the whole outline up front would need an outline schema and proto change and would ignore later steering |
| D6 | Router input | Lesson title, goal, topic title, level, locale, flags; at most 600 characters | Minimises learner content sent to a model; no chat, notes or free-text profile fields |
| D7 | Router thresholds | Per skill, default 0.9, tuned on the router golden set and re-checked each model change | A 2B router at 0.5 over-fires (`baseten/joey-1`, 4 of 12 exact; 8 of 12 at 0.7). Thresholds are data, not code |
| D8 | Guard architecture | One `input_guard` core plus four surface skills; adapters keep today's wire shapes and thresholds | Removes four copies of one policy; keeps API contracts; one place to add `self_harm` and `pii` |
| D9 | Guard model | The cheapest model that meets the guard golden set (false-positive ceiling 2 percent, zero missed attacks). Start from `tencent/hy4-flash` at effort none with the existing verdict schema (already accepted, ADR 0018); evaluate Decisions API models (`noul` for each reason code) as the cheaper, faster option | Safety judgement needs the policy and examples; decision models are cheaper and give probabilities but have no evidence on our attack set yet. The guard set decides, not preference |
| D10 | Fail behaviour | Creation and publication surfaces fail closed; tutor chat, notes and transcripts fail open after the deterministic pre-filter | Creation inputs are stored and re-used in later prompts (steering, brief), so a miss persists; chat text is read once, stays delimited as data, and holding the stream on a guard outage is a worse learner outcome than the residual risk. Matches the existing fail-closed rule for the four screens |
| D11 | Tutor latency | Guard runs in parallel; first token held up to 1000 ms | Spike and report: guard-sized calls take 1.5 to 3.5 s on Hy4 (including cold start), decision models 0.7 s; a 1 s hold covers a decision-model guard and bounds the worst case |
| D12 | Self-harm | `self_harm` verdict returns a localised safe response with approved resources, no main model call, no charge | A refusal is the wrong response to a distress signal; a main-model answer is unreviewed. Resource list and wording are owner-only |
| D13 | PII | Deterministic redaction of payment cards, government-id patterns and secret-like tokens in prompt copies; emails and phones flagged, not rewritten | Redacting names or emails breaks tutoring and note content; cards and secrets have no learning value |
| D14 | Who pays | Router and guard are platform-paid, inside protected cost, never a learner line item | Same as today's screens ("no learner quote or charge"); learner prices unchanged, so no adverse-change notice |
| D15 | Prompt layout | System = core then skills sorted by `order`, `id`; user = delimited data, persona, steering, repair last | Byte-stable prefix for caching; learner text never in system (data stays data) |
| D16 | Flexibility | Skill body at most 150 tokens, one example; structured outputs only where a consumer parses (ADR 0018); `max_tokens` from the ceiling; skill may raise reasoning effort within ceiling | Rule lists constrain the model and cost tokens; principles plus one example give the model room. Evidence is the spike: skills with one example lifted skill fit from 5/18 to 15 to 17 of 18 without changing validity |
| D17 | Rollout | `prompt_mode` per task, default `monolithic`, promoted by the economics gate with live evidence | Reuses ADR 0019; rollback is a config flip |
| D18 | First tasks | `lesson_slides` then `tutor_chat` | Largest output cost and largest call volume; tutor has the clearest cache win |
| D19 | Decide-by-rule examples | `character_cast`, `steering_focus`, `region_grounding`, `persona_style`, `age_band_tone`, `language_practice` (locale not `en` or language course flag) | Inputs exist; zero router cost |

Alternatives rejected: (a) load every skill always (spike arm B: +59 percent input tokens for the same fit as routing within noise; the cost grows linearly with library size); (b) let the main model pick its own skills from an index (extra generation, injection surface, no cache stability); (c) route the whole outline once at course creation (needs outline and proto change; stale after steering); (d) OpenRouter Guardrails as the input guard (regex patterns and a "Security" setting only; no classifier endpoint in the docs, and per-workspace config outside the repo; useful as defence in depth, not as the guard); (e) a separate guard per surface (duplicated policy, which is today's cost).

## 3. Skill modules

Format, one table per skill:

```toml
[[skill]]
id = "diagram_layout"            # lowercase snake case, unique per task
description = "The lesson explains a process, structure, flow or labelled figure that is best drawn."
tasks = ["lesson_slides"]
version = 1
order = 40                        # deterministic assembly order
tokens = 68                       # measured, verified within 10 percent by a test
default = false                   # member of the safe default set
conflicts = []
reasoning_effort_min = ""         # optional; raises, never lowers
body = """..."""                  # principle + one compact example, at most 150 tokens
```

Per task (v1 list; each needs a with-skill versus without-skill golden pair):

| Task | Core (always) | Skills (R = rule, M = model, D = in default set) |
|---|---|---|
| lesson_slides | Role, JSON shape, slide HTML contract, regions contract, test and variant_pool contract, data-as-data rule | `diagram_layout` (M,D), `code_slide` (M), `math_notation` (M), `comparison_table` (M), `timeline` (M), `narration_pacing` (D), `language_practice` (R), `age_band_tone` (R), `character_cast` (R, today's `segments` text), `steering_focus` (R), `region_annotation` (R: regions on, already core today) |
| tutor_chat | Role, data-as-data rule, grounding, say-when-unknown, stay on topic, plain text | `region_grounding` (R: `selected_region`), `persona_style` (R), `socratic_mode` (M or learner setting, D off), `worked_example` (M), `language_learning` (R), `source_citation` (R: `source_excerpts` present) |
| course_preview, course_outline, outline_replan, outline_chat_change | Output contract, preserve-ids rules, data-as-data rule | `depth_and_scope` (R), `maths_course` (R), `code_course` (R), `language_course` (R), `merge_vs_replace` (R, replan) |
| note_enrich, lesson_test, setup_chat, course_brief_chat, character_create | Contract only | None in v1; revisit when the A/B shows a gain |
| input_guard | Injection, unsafe content, self-harm, PII, benign look-alike rules, verdict schema | `steer`, `character`, `course_publish`, `review` (all R by surface) |

Spike skill bodies (5, 61 to 78 tokens each) are in `evals/spikes/prompt-routing/run.py` and are the starting text for group 3a.

## 4. Router design

```
RouterInput{task, title, goal, topic, level, locale, flags}  (no free text)
 1. rules(task, flags)      -> decided skills (R) + list of undecided M skills
 2. cache(hash(task, skills_version, router_version, input))  -> hit: stop
 3. Decisions API: state = RouterInput JSON, questions = {skill_id: noul(description)} for undecided M skills
      provider.data_collection = deny, timeout 800 ms, 1 attempt
      load skill when P(yes) >= threshold[skill]; cap by max_skills (probability order)
      store in cache, TTL 30 days
 4. on timeout / refusal / unknown key / breaker open -> task default set
```

- Cache: Redis (ADR 0003 stateless), disposable; skills version in the key invalidates on any skill change.
- Cost: spike router cost 0.0000089 USD per call (about 210 input tokens at 0.042 per 1M, from the reported cost), 2.6 percent of a 0.000342 lesson; ceiling 3 percent and 0.00002 USD per request, daily breaker at 1 percent of the day's engine spend.
- Observability: `router_source` per request, counters for unknown keys, timeouts, default-set rate (alert above 20 percent).
- Break-even versus loading everything: the router pays for itself when the skills it removes cost more input than the router. The five spike skills cost 348 tokens (0.0000122 USD uncached, 0.0000005 USD cached) and the router 0.0000089 USD, so at five skills the router costs about as much as it saves uncached and more than it saves when the prefix is cached. The break-even is about 250 uncached tokens removed per call (0.0000089 / 0.035 per 1M), reached at roughly 4 skills of 70 tokens skipped, so the router earns its cost only for libraries of about 10 or more skills per task (about 1,000 tokens, 0.000035 USD uncached, a quarter of which the router costs), or for quality. Rules-only routing has no overhead, which is why rules come first and why v1 keeps most skills rule-routed. The model router is worth enabling for `lesson_slides` content kinds because fit rises from 28 to 83 percent, not because it is cheaper.
- Honest limit: the Decisions API endpoint is alpha ("the shape may change"), its data-retention preference is not documented on the docs page (the spike request with `provider.data_collection = "deny"` was accepted by 6 of 6 models tried; that proves syntax, not enforcement), and some models are served by one provider. Hence the trait, the fallback to the task's own chat model with a strict enum schema, and the rule-first order.

## 5. Guardrail design

Surfaces and failure behaviour:

| Surface | Today | New | On guard failure | Latency budget |
|---|---|---|---|---|
| Course topic, brief answers, setup chat | none (setup/brief) | `input_guard` core | closed | 4 s (before generation) |
| Steering instruction | `steer_screen` | `input_guard` + `steer` | closed | 4 s (existing 10 s ceiling lowered after eval) |
| Outline chat request | none | `input_guard` core | closed | 4 s |
| Character prompt | `character_screen` | `input_guard` + `character` | closed | 4 s |
| Course publish | `course_screen` | `input_guard` + `course_publish` (own structured output) | closed (`pending_screening`) | existing |
| Review text | `screen_review` | `input_guard` + `review` (own structured output) | closed | existing |
| Quick note | none | `input_guard` core | open after pre-filter | async, off the critical path |
| Tutor chat message, voice transcript as chat input | none | `input_guard` core, parallel | open after pre-filter; hold first token up to 1000 ms | 1 s |

Pipeline: (1) deterministic pre-filter: hidden or bidi characters, length bounds, known injection phrases in the languages we support, card and secret patterns; match decides without a model; (2) model verdict `{allowed, reason_code}` with reason codes `prompt_injection`, `unsafe_content`, `self_harm`, `pii`, `off_topic_for_course`, `too_long`; (3) the main prompt still treats the text as delimited data. Output guardrails (validators, `leaks_prompt`, region and slide checks, steering echo checks) are unchanged.

Consolidation steps without behaviour change: introduce `input_guard` with the union of today's examples as per-surface skills; run old and new screens side by side in the guard golden set until parity (zero missed attacks, false-positive rate not above today's); only then route API calls to the new task. Existing verdict shapes remain at the wire.

No double charge: the guard is inside the existing platform envelope; a blocked request spends nothing from the learner's Credits; the main call is not made.

## 6. Prompt caching layout and flexibility

- Layout: `system = core + skills(order,id)`; `user = <data> ... </data>` then persona, steering section, repair turn. The prefix is identical for every lesson that selects the same skills, and the core alone is identical for all.
- OpenRouter docs (checked 2026-11-23): most providers cache automatically; OpenRouter uses sticky routing to keep a conversation on the provider that holds the cache; Hy4 Flash lists an `input_cache_read` price of 0.0015 per 1M tokens, so prefix tokens read from cache cost 4 percent of normal input. Explicit `cache_control` is only needed for Anthropic and Alibaba style providers, which are not in our route. Spike (section 9) confirmed automatic caching on this route: 84 percent of prompt tokens were cache reads for a shared prefix, 77 percent for the all-skills prefix and 42 percent when routing multiplies the distinct prefixes. Routing therefore fragments the cache: keep the number of distinct skill sets small (cap, sorted order, rules before model) and measure the hit share in `cached_input_tokens`.
- Expected tutor chat saving (the spike measured 84 percent hits for a single shared prefix, back to back): prefix 430 tokens, hit rate 70 percent within a thread burst, saving 430 x (0.035 - 0.0015) / 1M x 0.7 = 0.0000101 USD per message, 22 percent of the call. Lesson: core 548 tokens plus skills, saving about 0.000018 USD per hit, 5 percent of a lesson at full hit rate. Both are estimates until group 6 measures cached tokens in production usage.
- Flexibility rules are in the spec: principles plus one example, at most 150 tokens per skill, validator-bound rules only in the core, per-task model and reasoning overrides, skill-requested effort bump inside the ceiling, `max_tokens` from the quote ceiling (lesson cap 6,000, task minimum 1,200; a 1,961-token mean lesson with a 6,000 cap leaves headroom without letting a runaway generation exceed the ceiling), slide count from the learner's lesson length rather than "2 to 4 slides" prose.
- Deterministic fix-ups beat prompt prose: the 400 ms per word duration rule is enforced by clamping `duration_ms` in `prepare_lesson` (task in group 3a) instead of spending prompt tokens and repair calls on it.

## 7. Economics

Per-task expected deltas, from the baseline and the spike. "Router" is the model call only when rules and cache miss.

| Task | Prompt tokens vs today | Router + guard cost | Expected cost delta | Expected latency | Expected quality |
|---|---|---|---|---|---|
| lesson_slides | +73 to +116 input (1 to 1.4 skills of 61 to 78 tokens) | router +0.0000089 (2.6 percent) | spike: +6 percent (0.000308 vs 0.000291, inside noise); cache hit on core saves up to 0.000018; deterministic duration clamp avoids a repair call in 17 of 24 first attempts (saves up to 0.0003 per avoided repair) | router adds 0.7 s to a 9 to 10 s call (cached: 0) | skill fit 28 to 83 percent (spike) |
| tutor_chat | about 0 (region and persona paragraphs move to rule skills: minus 209 tokens for most messages) | guard 0.00001 to 0.00002 (parallel) | minus 0.0000101 cache plus minus 0.0000073 shorter prefix, plus 0.00002 guard | guard hidden under first-token time | grounding unchanged |
| course_preview, course_outline, outline_replan | 0 to minus 60 (rule skills) | none, rules only | about 0 | none | unchanged |
| screens (4 to 1) | minus 30 percent tokens (shared policy once) | none (these are the guard) | minus 25 percent per screen | same | parity gate |
| note_enrich, setup_chat, course_brief_chat, outline_chat_change | 0 | guard 0.00001 to 0.00002 | +0.00001 to +0.00002 (more than the 0.000008 to 0.000015 call itself) | none (note async) | safety up |

Break-even statements:

- Router: positive only when it removes more than 0.0000089 USD of input (254 tokens at 0.035) or avoids a repair or fallback (one avoided repair saves 0.00034 USD, so a 2.6 percentage point drop in repair rate pays for the router). With 5 skills of about 70 tokens the token saving alone is marginal; the case for the router is validity and skill fit (spike) plus library growth.
- Guard: is a cost, not a saving; it buys safety coverage on five new surfaces. Its ceiling is 0.00005 USD per text, 0.2 percent of the protected cost ceiling per credit (0.0015 USD).
- Gate: spec "Economics gate" blocks promotion when routed cost is more than 3 percent above monolithic without a 2 point quality gain.

Pricing: learner prices, credit amounts and pricing versions do not change (no adverse notice). `economics.go` protected-cost checks take the routed worst case as input bound (core plus the task maximum of the largest skills plus the guard ceiling); margins improve only through output and cache effects measured above.

## 8. Evals, analytics, rollout

- Evals: per-skill with/without golden pairs; router accuracy set (exact skill set, at least 8 inputs per skill, 3 adversarial per task); guard set (attacks, benign look-alikes at least equal in number, false-positive ceiling 2 percent); fallback cases; A/B harness `--arm monolithic|routed|both` with paired difference and noise margin from a repeated monolithic arm; promotion evidence per task and fingerprint (ADR 0019); offline fixtures in draft PRs.
- Analytics: `$ai_generation` properties listed in the `analytics` delta; ids, enums and numbers only; dashboards for router source share, default-set rate, guard block and timeout rate per surface, cache-hit share, cost per task by `prompt_mode`.
- Backup and data: none new. Skills are repo files; the router cache is disposable.
- Rollout: per task, spend order, behind `prompt_mode`; guard surfaces migrate one at a time after parity; rollback is a config flip.

## 9. Spike results

Full tables, method and caveats: `evals/spikes/prompt-routing/README.md` and `RESULTS.md`. Total live spend 0.0212 USD generation plus 0.0009 USD router bench (cap 0.30 USD), staging key mapped for the commands, nothing secret stored.

Setup: 12 lesson cases, three prompt arms on `tencent/hy4-flash` (effort none, `json_object`), run twice (24 lessons per arm); a live router bench on six Decisions API models.

| Arm | Input tok | Output tok | Cost per lesson incl. router (USD) | Latency p50 | Skill fit | Fully valid |
|---|---|---|---|---|---|---|
| A: production prompt | 587 | 1,985 | 0.000291 | 8.8 to 9.6 s | 5/18 | 7/24 |
| B: production + all 5 skills | 935 | 1,801 | 0.000274 | 7.4 to 8.8 s | 17/18 | 7/24 |
| C: production + routed skills | 682 | 2,021 | 0.000308 | 8.8 to 9.7 s | 15/18 | 7/24 |

Findings:

1. Skills work: fit rises from 28 to 83 to 94 percent with one principle-plus-example paragraph of 61 to 78 tokens per skill. Routing keeps most of that at a fifth to a third of the added input.
2. Cost is flat within noise at this library size (differences 6 percent, standard error about 4 percent). Do not expect prompt skills to cut the lesson bill; they buy quality. The router costs 2.6 percent of a lesson at the best model and is the cost the gate watches.
3. The one failure is the duration rule (17 of 24, all arms). A deterministic clamp removes a repair call; this is the largest cost and latency lever the spike found (a repair doubles the call).
4. Caching works automatically on the Hy4 route (84 percent cache reads for a shared prefix) and routing fragments it (42 percent); the layout rules in section 6 exist for that reason.
5. The Decisions API is a good router: 0.7 s p50, 0.000003 to 0.000045 USD per request, `data_collection: deny` accepted. Thresholds matter more than model choice: 0.5 over-fires on small models, 0.9 is best on five of six.
6. Router accuracy 11/12 exact (best model, tuned on the spike set); 4/12 for the cheapest model at 0.5.

### Expected savings (honest summary)

| Source | Direction and size | Confidence |
|---|---|---|
| Prefix cache on tutor chat (stable system prefix, 430 tokens) | minus about 0.00001 USD per message (22 percent of the call) | medium: spike measured 84 percent hits back to back |
| Prefix cache on lessons (core 548 tokens) | up to minus 0.000018 USD per lesson (5 percent) | medium |
| Deterministic duration clamp | avoids a repair call in about 70 percent of first attempts: up to minus 0.0002 USD per lesson on average (about 70 percent of a lesson cost) if production repairs on every duration failure | needs production repair rate; high if confirmed |
| Skills (content kinds) | plus 0.000004 USD input, plus 0.0000089 USD router per lesson (about 4 percent) | high |
| Guard on five new surfaces | plus 0.00001 to 0.00002 USD per screened text; platform-paid | medium |
| Screen consolidation (4 to 1) | minus about 25 percent tokens per screen call (0.000003 USD) | low value, high maintenance value |
| Dynamic `max_tokens` | bounds runaway outputs; no mean change expected | medium |

Net: tutor chat becomes about 15 to 20 percent cheaper after guard, lessons are neutral to slightly cheaper (clamp) and clearly better on skill fit, and the guard adds safety coverage. Margins improve only marginally because AI cost is already about 0.0003 USD per lesson against a 0.0015 USD protected ceiling per credit; the case for this change is quality, safety and reliability, with cost held roughly flat.

## 10. Risks

- Decisions API is alpha and shapes may change: trait, fallback and default set bound the impact.
- Small decision models over-fire at 0.5: thresholds are data, tuned and gated on the router golden set (the spike tuned on its own 12 cases, so its 11 of 12 is an upper bound).
- Skill interaction: two skills can conflict on layout slots; `conflicts` and the cap limit it, the golden pairs catch it.
- A guard false positive blocks a benign learner: ceiling 2 percent plus an appeal-free retry message; false positives are counted per surface and reviewed weekly.
- Cache hit rate may be lower than assumed on a single-upstream provider; the economics gate uses measured cached tokens, not the estimate.

## 11. Implementation notes (groups 3a, 4, 5 started)

- Router model (checked 2026-11-24 against the OpenRouter catalog, `output_modalities=decisions`): `microsoft/microsoft-decision-1.1`, 0.042 USD per 1M input tokens, 32,768 context, endpoint `/api/alpha/decisions`; chosen on the spike bench (11 of 12 exact at threshold 0.9, 0.0000089 USD and 0.73 s per request, section 9). `liquid/d1.1` expires 2026-11-24, which removes it from the candidates; `liquid/d1.2` (0.02) and `perplexity/pplx-decider-flash-v1.3` (0.01) are the cheaper alternatives to measure on the router golden set. Measured cost on the live arm will be recorded here after the first routed A/B (task 9.1); until then the spike numbers stand. Request: one `noul` per allowed skill id, `state` = `RouterInput` JSON (no free text, at most 600 characters), `provider.data_collection = deny`, 800 ms timeout.
- Selection (`engine_core::router::select`): rule skills first (`narration_pacing` always, `language_practice` by locale, `character_cast` by cast, `age_band_tone` by learner level), then the decision cache (SHA-256 over task, skill ids and versions, router version, thresholds, bounded input), then the model, then the default set of skills marked `default`. The model's answer is filtered to the allowed candidate ids (unknown and rule-managed ids dropped, non-finite probabilities dropped), thresholded per skill (default 0.9) and ordered by probability so the cap in `Library::assemble` drops the least likely. Timeout, error or no router installed never fail generation and are not cached. The cache is process-local (`MemoryCache`) behind `DecisionCache`; the Redis backend and the daily budget breaker remain open (task 4.2).
- Input guard (`engine_core::guard`): deterministic pre-filter (hidden and bidi characters, injection phrases in English and Vietnamese, Luhn-checked card numbers, known secret shapes, per-surface length bound), strict verdict parser (`{allowed, reason_code}`, anything else is an error), per-surface failure rule (creation and publication closed, tutor chat and quick note open, 1000 ms hold on tutor chat), `redact` for prompt copies. Decisions carry a reason code and source only, never the text. The classifier model and the API wiring (tasks 5.2, 5.3, 5.5) are not built; the owner decision for the self-harm copy is issue #423.
- `course_preview` truncation: the preview prompt only said "return a short sample" and used no schema, against a 6000-token cap and a measured mean of 4,938 output tokens, so a long tail exceeds the cap and the JSON is cut ("EOF while parsing"; the provider already records `finish_reason=length`). The prompt now bounds the sample (at most 3 units of 2 lessons, exactly 2 slides, narration at most 40 words); confirm on the next live run that output tokens drop and `delivered` holds.

## 12. Input guard implementation (tasks 5.2 to 5.5)

- Classifier model (checked 2026-11-24 against the OpenRouter catalog): `input_guard` runs on the chain of the four screens it consolidates, `tencent/hy4-flash` with `z-ai/glm-5.3-flash` as fallback, reasoning `none`, strict `json_schema` output `{allowed, reason_code}` (`reason_code` enum: empty plus the six codes). Reason: parity first. Catalog alternatives looked at: `openai/gpt-oss-safeguard-20b` (0.075 in / 0.3 out per 1M, reasoning mandatory, so it spends tokens before the verdict and cannot meet a 1 s hold), `meta-llama/llama-guard-4-12b` (0.18 / 0.18, no structured output parameter, fixed taxonomy that has no injection or off-topic code), and the decision models (`liquid/d1.2` 0.02, `baseten/joey-1` 0.01, `microsoft/microsoft-decision-1.1` 0.042 per 1M input, no output cost).
- Measured cost (from the committed live report `evals/promotion/live-report.json`, the four existing screens on the same chain): 343 to 476 input tokens and 10 to 25 output tokens per call, 0.0000134 to 0.0000200 USD, latency 1.09 to 2.18 s (median about 1.5 s). `input_guard` adds a longer policy (about the same size) so the same range applies; the first live run of the `live-guard-*` rows records the actual number. Against a lesson of about 0.000342 USD the guard is 4 to 6 percent, which is why free-text surfaces only (not every request) and why the deterministic pre-filter runs first.
- Latency finding: a median of 1.5 s misses the 1000 ms hold on tutor chat, so on that surface the guard mostly ends in the fail-open branch (pre-filter only). Options, to be measured on the guard golden set before 5.3 is switched on for tutor chat: run the verdict on a decision model (`liquid/d1.2`, 0.73 s class latency in the router spike) through the existing `DecisionsRouter`, or accept the open branch for tutor chat and rely on the pre-filter plus delimited data. Closed surfaces (4 s budget) are unaffected.
- Prompt: trusted core policy plus one short surface skill in `engine-core::guard::classifier` (steer, character, course_publish, review, tutor_chat; core only for course_topic, outline_chat, quick_note). The skills stay in Rust next to the verdict parser until parity with the four screens is proven, then they can move to `engine/config/skills/input_guard.toml` (the loader already accepts that task).
- Wire: `EngineService.GuardInput(surface, text, course_topic)` returns `{allowed, reason_code, source, guard_version, attempts}`; the engine runs the pre-filter first (no model, no promotion evidence needed), then the classifier, then the surface failure rule. A live engine classifies only with passing live `input_guard` evidence (same `live_model` gate as every other task); without it the surface's failure rule decides. `input_guard` is not yet in `LAUNCH_TASKS` or `GATED` so the missing dataset rows do not fail `engine:evidence --require-launch`; add it there when the rows below are in a published dataset revision.
- API: `engine.Client.EnableGuard` (set by `INPUT_GUARD_ENABLED=true`, off by default) puts the guard in front of the main call of PreviewCourse, SetupChat, CourseBriefChat, OutlineChatChange, EnrichNote (through `GuardedRPC`) and Chat. A refusal is a typed `BlockedError` (gRPC FailedPrecondition, no main call). An outage follows the surface rule locally (closed: refuse; tutor chat and quick note: allow). Tutor chat answers HTTP 422 `input_blocked` with the reason code; for `self_harm` it adds `safe_response`, releases the hold (no Credits) and leaves the thread usable. The safe-response strings are placeholders without numbers or organisations until issue #423. The four existing screen RPCs (steer, character, course publish, review) are untouched: the consolidation switch waits for parity on the golden set (task 5.3).
- Analytics: `input_guard_decision` with `surface`, `allowed`, `reason_code`, `guard_source` (`pre_filter`, `model`, `failure`) and `latency_ms`; closed enums only, never text.
- Golden rows: 14 `live-guard-*` rows in `pending-prompt-skills.jsonl` (attacks decided by the pre-filter, self-harm, off-topic, grading attack, unsafe request, and benign look-alikes including a study question about suicide risk and a Vietnamese topic). A unit test keeps the pre-filter rows in step with the pre-filter.

## 13. Router state in Redis (task 4.2)

- `engine-providers::state` installs a Redis-backed `DecisionCache` (key `duolify:router:decision:<sha256>`, 30 day TTL) and `Breaker` when `REDIS_URL` is set (the compose engine service now passes it); otherwise the process keeps its memory cache and no breaker. Operations time out at 100 ms; a Redis error is a cache miss.
- Breaker: counters `duolify:router:spend:<utc day>` (an estimate of 9,000 nanos per model call, since the Decisions API returns no cost to us) and `duolify:engine:spend:<utc day>` (the spend of every bounded run, in `eval::run_bounded_formatted`). Open when router spend is above 0.001 USD and above 1 percent of the day's engine spend; open means the default skill set (`router_source = breaker`), never a failed generation. If the counters cannot be read the breaker is open (the optional model is skipped).
- Not measured: a real Redis round trip (no Redis in this environment; the unit tests run the same code over a fake `Kv`). The first routed live run on a deployment with Redis should confirm `router_source = cache` on a repeated lesson.

## 14. Decisions after the first live runs (guard latency, usage accounting, gate retry, model table)

- Guard latency on tutor chat (conflict: 1000 ms hold against 1.09 to 2.18 s measured on the screening model). Options: (A) a faster classifier, (B) run the guard in parallel with generation and withhold only the first tokens, (C) deterministic-only on tutor chat with asynchronous classification. B contradicts the safety intent: a `self_harm` verdict must mean no main call and no charge, and B has already started the main call. C alone leaves the learner without support when the text is a self-harm expression the pre-filter cannot see. Chosen: A plus the safe tail of C. On tutor chat and quick notes the classifier runs on a decision model (candidates `liquid/d1.2` at 0.02 per 1M, `microsoft/microsoft-decision-1.1` at 0.042; the router spike measured 0.73 s and 0.0000089 USD per request) with one yes/no question per reason code, an 800 ms hold; the closed surfaces (4 s budget) keep the chat classifier. If the verdict is late the turn proceeds (fail open, as the spec says), the verdict still completes in the background, and a late `self_harm` verdict appends the safe-response message to the thread. Not built yet: it needs the guard golden rows run on both classifiers to pick the model, which is a live run (command in the rollout runbook; add `liquid/d1.2` as `[tasks.input_guard_fast]`).
- `usage_unavailable` on failover. Today any attempt with unknown usage makes the case critical, even when a later attempt delivered with measured usage (seen once on `steer_screen`). Proposal, not yet implemented because it changes gate semantics: an attempt with unknown usage is priced at its envelope's worst case (`run_bounded` already does this for the learner quote), which keeps cost bounded and honest; the case is critical only when the delivered completion's own usage is unknown (the amount settled against the learner), and an earlier unknown attempt is reported as a warning with its worst-case cost. Estimating from tokens would invent a number the provider never reported.
- Promotion gate retry. Proposal: after a full live run, rerun only the failed cases once, still requiring 100 percent, and record both attempts in the report so a flaky case is visible rather than hidden. Evidence from five runs of #394: `live-lesson-regions-question-fractions` failed in 2 of 5 runs after failover from `tencent/hy4-flash` to `z-ai/glm-5.3-flash` (structure checks). Implementation waits for the owner's decision.
- Model table for `lesson_slides` (report only, no model change). Needs live runs: the same lesson cases (`lesson_slides` rows of the golden set plus the pending skill rows) on the current primary and one or two alternatives, compared with `mise run engine:compare` (pass rate, cost, latency p50 and p95, paired difference). The engine reads `models.toml` at compile time, so each alternative is a throwaway branch that changes `[tasks.lesson]` `model`; the controller runs it and the table goes here. No row exists yet.

## Owner questions

1. Self-harm response: which support resources and wording per supported locale (English, Vietnamese, Spanish), and who reviews them. The engine returns a code; the API shows owner-approved copy.
2. The Decisions API is alpha. Is a dependency on it acceptable as the first router source (with fallback to Hy4 and default sets)? The default decision, if there is no answer, is yes behind the trait, because failure cost is bounded.

## 15. Caching layout (checked 2026-11-24 against OpenRouter and Anthropic docs)

- OpenRouter prompt caching: OpenAI, Grok, Moonshot, DeepSeek, Z.AI (the GLM fallback) and Gemini 2.5+ cache implicitly; Anthropic, Alibaba Qwen and Gemini explicit need `cache_control` breakpoints. Hy4 is not on the docs list; the spike measured automatic hits on this route, so the engine adds no marker for it. Provider sticky routing hashes the first system message and the first non-system message of a request, so a byte-stable system prefix keeps one conversation (and one skill set) on one provider for up to 60 minutes of inactivity. Stable content first, variable content last is the documented layout and is what `Library::assemble` produces.
- Direct Anthropic route (not in the launch route): a routed prefix is sent as one `system` text block with `cache_control: {type: ephemeral}` (the Messages API needs the array form for a block-level breakpoint; minimum cacheable length 512 tokens on current 5.x models, shorter prompts run uncached without error). Monolithic prompts send no breakpoint.
- Cached tokens: `usage.prompt_tokens_details.cached_tokens` is read into `Attempt.cache_read_tokens` and settled at the cache-read rate (`credits::check_usage`, API settlement). Not yet measured on a routed run: task 6.1 waits for the controller's live A/B.
- Economics (`economics.go`): protected cost prices the quote input plus the routed skill budget (`routedPrefixTokens`); output is bounded by `eval::routed_max_tokens` (lesson cap 6,000, minimum 1,200). No learner price or pricing version changes.
