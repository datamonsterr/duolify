# Design: lesson-quality-pipeline

## 1. Audit (2026-11-24)

### Pipeline today

| Step | Code | Route | Live numbers (`evals/promotion/live-report.json`) |
|---|---|---|---|
| Brief | scripted `course.brief` (ADR 0024), vague check `courses/mod.rs` | Hy4 Flash, no reasoning | small |
| Preview: title, Units, Lesson goals, sample Slides | `courses::preview_prompt` | `preview`, Hy4 Flash low | 4.1k output tokens, 26 s, one call |
| Outline edits | `outline::prompt` (`outline_chat_change`) | Hy4 Flash | 1.8 s |
| Create Course | `outline::creation_prompt`, `engine-server` `generate_course_outline` | `outline`, Hy4 Flash low | echo of the input, discarded |
| Lesson | `lessontest::lesson_prompt` (+ `regions::shape`), `generate_lesson` | `lesson`, Hy4 Flash none, GLM 5.3 Flash fallback | 1.0 to 2.6k output tokens, 6 to 12 s, about $0.0003 |
| Narration | `narration/` | ElevenLabs (OpenRouter TTS in PR #412) | likely the main per-Lesson cost |

The next Lesson is prepared after a pass (E4-05), so lesson latency is mostly hidden. The preview is the visible wait.

### Rubric and scores

Each criterion scores 0, 1 or 2. "Generated" is inferred from the prompts, validators and eval reports. Live sampling was blocked by an OpenRouter 401 during the audit; task 6.4 re-scores it on live samples. "Authored" is the two example Courses in `services/api/internal/examples/content`.

| # | Criterion (source) | Static or judge test | Generated | Authored |
|---|---|---|---|---|
| R1 | Measurable objective with an observable verb (Anderson & Krathwohl 2001) | verb list lint on goal and objective | 1 | 2 |
| R2 | Prerequisite order, difficulty rises (Merrill 2002; Kalyuga 2003) | judge on the outline: no term used before it is taught | 1 | 2 |
| R3 | Each objective tested, each question tied to an objective (Wiggins & McTighe 2005) | `topic_ids` coverage | 2 | 2 |
| R4 | Activation: hook or recall first (Merrill 2002) | judge on Slide 1 | 0 | 2 |
| R5 | Worked example before the check (Sweller & Cooper 1985; Renkl) | judge: example region before question region | 0 | 1 |
| R6 | Retrieval practice with explanatory feedback (Roediger & Karpicke 2006; Hattie & Timperley 2007) | every wrong choice has a reason | 1 | 1 |
| R7 | Distractors are plausible misconceptions (Haladyna et al. 2002) | judge labels a misconception per distractor | 0 | 1 |
| R8 | Spaced review of earlier topics (Cepeda et al. 2006; Dunlosky et al. 2013) | share of questions on earlier Lessons | 0 | 0 |
| R9 | Mayer coherence, signalling, segmenting, redundancy (Mayer 2021) | words per Slide at most 40; narration-to-screen word overlap at most 0.6; one idea per Slide | 1 | 2 |
| R10 | Pace and length (Guo et al. 2014; Mayer segmenting) | 110 to 200 wpm; Slide count matches the length setting | 1 | 2 |
| | Total | | about 7/20 | 15/20 |

Measured on the authored Courses: 4 Slides per Lesson; 20 to 55 visible words per Slide; 133 to 140 wpm; 0.29 to 0.43 narration-to-screen overlap; 0 of 32 questions on an earlier Lesson; some distractors not plausible ("The users are wrong").

The source list came from a research pass that did not open the primary papers, so the DOIs are not verified yet; check them before quoting this rubric outside the repo. Mayer, *Multimedia Learning* 3rd ed. (2021), doi:10.1017/9781316941355. Cowan 2001 (a working memory of about 4 chunks; Miller's 7±2 is outdated). Pashler et al. 2008: no evidence for "learning styles", so no per-style variants. The 40-word Slide cap and the 3 to 7 minute Lesson length are conventions, not hard findings.

## 2. Decisions

| # | Decision | Why |
|---|---|---|
| D1 | Repurpose the paid outline call as the Course planner. Output is the confirmed outline unchanged plus one `LessonPlan` per Lesson. The validator still rejects any change to ids, order, titles or goals, and any Slides, tests or HTML. | The Learner already pays for this step. A plan made once with the whole Course in view gives every Lesson its objective, prerequisites, misconceptions and review targets. The cheap per-Lesson model then writes against a plan instead of guessing. |
| D2 | `course_plan` route: candidates are `anthropic/claude-sonnet-5.5`, `anthropic/claude-haiku-5.5` and `tencent/hy4-flash` at medium reasoning. The A/B in `engine-eval` picks the cheapest candidate within 0.05 of the best judge score on the plan rubric. Until evidence exists, the route stays `tencent/hy4-flash` at low reasoning. | Per-Course one-off: about 5k output tokens is at most about $0.05 on Sonnet. Route changes need live promotion evidence (ADR 0019). |
| D3 | Lesson generation stays on Hy4 Flash. It gets the plan plus the teaching core. | Lowest cost per Lesson. The plan does the hard thinking. Revisit only if the R1 to R10 judge scores stay below the thresholds. |
| D4 | The teaching contract goes in the always-on core (`LESSON_SHAPE` successor), not in a routed skill. | It must apply in `monolithic` mode, which production runs today. It costs about 120 tokens. |
| D5 | Clarifying questions come from the preview call (no extra call), are capped at 3 with at most 4 options each, and are omitted when the brief settles the outline. Answers become outline-chat requests through the existing quoted proposal flow, and are stored on the draft for the planner. | Reuses the quote, proposal and undo paths. The structure changes only with Learner consent. |
| D6 | Lesson length: `short` is 3 Slides and 2 questions, `medium` 5 and 3, `long` 8 and 5. Default `medium`. The value is per Course and editable later; the change applies to Lessons not yet generated. | Fits the current `lesson_content` envelope (8 Slides, 5 questions), so no cost-ceiling change. |
| D7 | Estimate for one Lesson = `lesson_content` price for the length + `narration` price when narration is on, from the active pricing version. Optional `lesson_content_by_length` prices default to the single price. | Shows a real number now with no price change. Tiering stays an owner pricing decision. |
| D8 | Review questions: up to 2 per Lesson test, chosen by the API from stored variants of earlier Lessons in the plan's review targets, weakest topic first (most wrong answers, then oldest pass). They are non-gating, marked `review`, and recorded. | Spacing benefit with no generation cost and no change to pass rules. Half-life scheduling can come later. |
| D9 | Static quality checks are warnings in production (analytics, no repair turn) and critical in evals. | A repair turn costs a second call, and the checks are heuristics. Evals hold the quality line. |
| D10 | The LLM judge runs offline only (nightly and promotion). One binary call per criterion, evidence quote required, judge model family different from the generator (`anthropic/claude-haiku-5.5`), rubric anchors from the authored Courses and degraded copies. A judge change needs Cohen's kappa of at least 0.6 on 20 or more owner-labelled anchors. | Pointwise binary per criterion is the most stable form. Avoids self-preference and length bias. |
| D11 | Lesson context = compact JSON `{outline, lesson, plan, learner, lesson_length, history (last 5), weak_topics}`. | Fewer tokens, stable shape, no limits or deadlines in the prompt. |
| D12 | The narration bound is 3.3 words per second, and the prompt says at least 300 ms per word. | The validator and prompt agree, about 200 wpm at most. |

## 3. Data

- `lesson_plans`: jsonb column on Lesson stubs, written once by the planner, immutable. Course data: backups, export and erasure as today.
- `courses.lesson_length` enum, default `medium`.
- Draft `clarifications`: up to 3 `{question_id, answer}` on the outline draft. Same retention as the draft (30 days idle).
- Test sessions record review questions with `kind = review`.

## 4. Analytics (PostHog, ids and enums only)

- `outline_clarify_shown` {question_count}, `outline_clarify_answered` {question_id, skipped}
- `lesson_length_changed` {course_id, from, to}
- `lesson_estimate_shown` {course_id, lesson_length, credits, surface}
- `review_question_answered` {lesson_id, source_lesson_id, correct}
- `$ai_generation` adds `lesson_length`, `plan_present`, `quality_warnings` (codes)

## 5. Rollout

Each piece ships behind a flag: `course_plan`, `outline_clarify`, `lesson_length`, `review_questions`. The plan and teaching core ship together after the eval gate passes on the live golden set.
