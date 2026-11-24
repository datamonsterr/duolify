Future implementation checklist; this change delivers specification and a measured spike only. Each group is one PR (sizes: S under 200 lines, M 200 to 600, L 600 to 1500). TDD order inside each group: e2e, integration, unit, then code. Every group keeps `prompt_mode = "monolithic"` until group 8, so merging a group never changes live behaviour.

## 0. Spike and decisions (this change)

- [x] 0.1 Baseline from `evals/promotion/live-report.json` and `engine-core` prompt text (see `design.md`).
- [x] 0.2 Live spike `evals/spikes/prompt-routing/` (12 lesson cases, 3 prompt arms, live Decisions API router) with `RESULTS.md`.
- [ ] 0.3 Owner answers the owner-only questions in `design.md` (self-harm support resources and wording; confirm the Decisions API alpha dependency is acceptable behind a fallback).
- [x] 0.4 ADR recording the decisions table; `CONTEXT.md` terms **Skill**, **Router**, **Input guard**.

## 1. Skill module loader and fingerprint (M)

- [x] 1.1 E2E: a task with `prompt_mode = "monolithic"` produces byte-identical prompts to today for every golden fixture (guards all later groups).
- [x] 1.2 Integration: `engine/config/skills/*.toml` load at startup; unknown task, duplicate id, missing field, conflicting pair and over-budget set fail the boot check with a typed error.
- [x] 1.3 Unit: deterministic assembly (order, de-dup, cap, budget), token-count drift test (10 percent), skill files in `promotion::SOURCES` and `fingerprint_sources_cover_every_ai_module`.
- [x] 1.4 Implement `engine-core::skills` (loader, `assemble(task, ids, data) -> Prompt{system, user}`), config tables `[routing]`, `prompt_mode`; no skill content yet.

## 2. Provider system/user split and dynamic max_tokens (M)

- [x] 2.1 Integration: `request_model` sends `system` (prefix) and `user` (data) for routed prompts and the single `user` message for monolithic prompts; steering policy envelope unchanged.
- [ ] 2.2 Integration: `max_tokens` = min(task cap, ceiling-derived output) with task minimum; usage records `cached_input_tokens` and bills the cache-read rate.
- [x] 2.3 Unit: layout has no volatile value before the first user byte; same prefix across two requests is byte-identical.
- [x] 2.4 Implement; replace Rust `Debug` rendering of the lesson request (`format!("{context:?}")` in `engine-server`) with compact JSON behind the same switch and measure the token delta on the golden fixtures.

## 3. Split prompts task by task, ordered by spend (L, three PRs)

Order from the baseline: `lesson_slides` (largest per-call cost, 81 percent output tokens) and `tutor_chat` (highest call volume, input heavy, clearest cache win) first, then `course_preview`, `outline_replan`, then `course_outline`, `lesson_test`, `outline_chat_change`, `note_enrich`, `setup_chat`, `course_brief_chat`, `character_create`.

- [ ] 3.0 Deterministic duration clamp in `prepare_lesson` (spike: the 400 ms per word rule fails in 17 of 24 first attempts); drop the rule's prose once the clamp is proven by golden cases (S, ships with 3a).
- [ ] 3.1 (3a, L) `lesson_slides`: core plus skills `diagram_layout`, `code_slide`, `math_notation`, `comparison_table`, `timeline`, `narration_pacing`, `language_practice`, `age_band_tone`, `character_cast`, `steering_focus`. Golden pair per skill; rule skills (`character_cast`, `steering_focus`, `age_band_tone`, `language_practice`) wired to existing request fields.
- [ ] 3.2 (3b, M) `course_preview`, `outline_replan`, `course_outline`, `outline_chat_change`: core plus `depth_and_scope`, `language_course`, `maths_course`, `code_course`, `merge_vs_replace` (replan).
- [ ] 3.3 (3c, M) `tutor_chat`: core plus `region_grounding` (rule), `socratic_mode`, `worked_example`, `language_learning`, `source_citation`, `persona_style` (rule); `note_enrich`, `setup_chat`, `course_brief_chat`, `lesson_test`, `character_create` stay core-only unless the A/B shows a gain.
- [ ] 3.4 Each split: E2E golden suite green on both arms; skill bodies at most 150 tokens with one example; no body embeds learner text.

## 4. Router: rules, cache, model (L)

- [ ] 4.1 E2E: lesson generation with a diagram topic loads `diagram_layout`; router outage loads the default set and the lesson still validates.
- [x] 4.2 (client, cache, fallback; Redis backend and daily breaker pending) Integration: Decisions API client behind `SkillRouter` trait (noul per undecided skill, threshold per skill, `provider.data_collection = deny`, 800 ms timeout, refusal and unknown-key handling) plus chat-model enum fallback; Redis cache with 30 day TTL keyed by (task, skills version, router version, input hash); daily budget breaker.
- [ ] 4.3 Unit: rule table per task, source precedence, cap and budget trimming, router input contains no free text (type-level: only `RouterInput` fields), counters.
- [ ] 4.4 Implement; mock router for offline evals; recorded fixtures for the decision model.

## 5. Input guardrail consolidation (L)

- [ ] 5.1 E2E: attack set blocked on every surface; self-harm text returns the safe response without a main call or charge; benign look-alikes pass.
- [ ] 5.2 Integration: `input_guard` core plus surface skills `steer`, `character`, `course_publish`, `review`; existing screen RPCs keep contracts and thresholds; fail-closed on creation surfaces, fail-open with 1000 ms hold on tutor chat, notes and transcripts; deterministic pre-filter; PII placeholders.
- [ ] 5.3 Integration (API): safe-response path for `self_harm` with approved localised resources, no Credits, thread stays usable; API stops calling four screen tasks separately only after parity is proven on the golden set.
- [ ] 5.4 Unit: pre-filter patterns, verdict parser, reason-code enum, per-surface failure table, cost ceiling.
- [ ] 5.5 Choose the guard model with the guard golden set (candidates in `design.md`); record it in `models.toml` `[guard]`.

## 6. Caching layout and cost gate (S)

- [ ] 6.1 Integration: two lessons with the same skills report cached input tokens against recorded usage; cache-read billing matches `prices.toml`.
- [ ] 6.2 Unit: `services/api/internal/pricing/economics.go` protected-cost check uses the routed worst-case input bound; fixture for each routed task.
- [ ] 6.3 Implement the bound in `engine_route_check.go` and the quote path; no pricing version change.

## 7. Eval harness and golden cases (L)

- [ ] 7.1 E2E: `duolify-eval --arm both` on recorded fixtures emits per arm tokens, cost, latency p50 and p95, pass rate, paired difference and noise margin.
- [ ] 7.2 Integration: router accuracy case kind, guard case kind (false-positive ceiling), fallback cases (router timeout, refusal, unknown key, breaker open; guard malformed and outage).
- [ ] 7.3 Unit: scoring, ceilings, adversarial router inputs; add cases to the Hugging Face dataset and bump `pinned-datasets.toml`.
- [ ] 7.4 Update `evals/README.md` and `thresholds.toml`.

## 8. Analytics and dashboards (S)

- [ ] 8.1 Integration: `$ai_generation` carries the new properties, no text, honours opt-out.
- [ ] 8.2 PostHog dashboard: router source share, default-set rate, guard block and timeout rates by surface, cache-hit share, cost per task by `prompt_mode`; alerts on default-set rate above 20 percent and guard timeout above 5 percent.

## 9. Rollout behind a per-task switch with promotion evidence (M)

- [ ] 9.1 For each task in spend order: run the A/B live, commit the routed live report, flip `prompt_mode = "routed"` in its own PR when the economics gate passes; otherwise leave monolithic and record the numbers.
- [ ] 9.2 Roll back procedure documented in `docs/` and tested: flipping the switch restores the previous prompt and ignores cached decisions.
- [ ] 9.3 Update `docs/agents/skills.md` pointers, `.env.example` (no new variable expected; the Decisions API uses `OPENROUTER_API_KEY`).
