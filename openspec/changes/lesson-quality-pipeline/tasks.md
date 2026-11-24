Each group is one ticket and one PR. TDD order inside each: e2e, integration, unit, code.

## 1. Teaching core and lesson context (engine only; ticket LQ-1)

- [ ] 1.1 Unit tests: the lesson prompt contains the teaching contract and the length targets; context JSON has only the allowed keys; no `limits` or deadline text; history capped at 5.
- [ ] 1.2 Rewrite `LESSON_SHAPE` with the teaching contract (design D4, about 120 tokens) and slide and question targets taken from a `lesson_length` argument (default `medium`). Same text in `routed_lesson_prompt` core.
- [ ] 1.3 Replace `format!("{context:?}")` in `engine-server` `generate_lesson` with a compact JSON builder (D11).
- [ ] 1.4 Narration bound 3.3 words per second; the prompt says at least 300 ms per word (D12). Update fixtures that break.
- [ ] 1.5 Static quality checks (visible words per Slide, wpm 110 to 200, narration-to-screen overlap at most 0.6): return warning codes; record them on `$ai_generation` `quality_warnings`; never trigger a repair.
- [ ] 1.6 `mise run engine:lint`, `engine:test`, `engine:eval` (offline) pass; update the promotion fingerprint.

## 2. Course plan (proto, api, engine; ticket LQ-2)

- [x] 2.1 E2E: create a Course from a confirmed outline (mock engine) and its Lesson stubs carry plans; the first generated Lesson request carries its plan.
- [x] 2.2 Integration: a plan that changes the structure, points forward or is missing is rejected with bounded fallback; Credits released on terminal failure; plans immutable.
- [x] 2.3 Proto: `LessonPlan` on `LessonRef`; `clarifications` on `GenerateCourseOutlineRequest`; `buf lint`, `buf breaking`.
- [x] 2.4 Engine: planner prompt (`outline::creation_prompt` successor) and validator; strict JSON schema in `structured.rs`; fixture provider output.
- [x] 2.5 Engine: `course_plan` route in `models.toml` (stays `tencent/hy4-flash` low until 6.3), prices for candidates in `prices.toml`, alias so quotes keep matching.
- [x] 2.6 API: goose migration `lesson_plans` jsonb on Lesson stubs (rename after the newest migration on main before merge); store on create; send on `GenerateLesson`.
- [x] 2.7 Data inventory, backup and export entries.

## 3. Lesson length and Credit estimate (proto, api, engine, client; ticket LQ-3)

- [ ] 3.1 E2E: set `short` in the brief, then Course settings shows "About N Credits per Lesson"; change to `long`, the estimate updates and the next Lesson has 8 Slides (mock engine).
- [ ] 3.2 Integration: `courses.lesson_length` seeded from the brief, default `medium`; change applies to ungenerated Lessons only; estimate endpoint uses the active pricing version and narration setting; no reservation.
- [ ] 3.3 Proto `lesson_length` on `GenerateLessonRequest`; engine passes it into 1.2.
- [ ] 3.4 Pricing: optional `lesson_content_by_length` in the schema and resolver; quotes use the Course length (pricing-config delta). No price values change.
- [ ] 3.5 Client: estimate chip on preview, outline confirm and Course settings; length control in Course settings; copy in all locales.
- [ ] 3.6 Analytics `lesson_length_changed`, `lesson_estimate_shown`.

## 4. Outline clarifying questions (engine, proto, api, client; ticket LQ-4, after PR #427)

- [ ] 4.1 E2E: a vague-depth brief gives a preview with questions; answering one shows a quoted outline proposal; accept changes the outline; skip-all still creates the Course.
- [ ] 4.2 Skill `outline_clarify` in `engine/config/skills/course_preview.toml` (at most 150 tokens, one example, rule-routed default-on). The preview core and schema add `clarifying_questions` (0 to 3, 4 options max).
- [ ] 4.3 Proto `clarifying_questions` on the preview response; API stores answers on the outline draft (30 days idle, erasure cascade) and sends them as `clarifications` (2.3).
- [ ] 4.4 Client: questions in the outline review chat with quick replies and Skip; an answer calls the existing outline-chat proposal path.
- [ ] 4.5 Analytics `outline_clarify_shown`, `outline_clarify_answered`.

## 5. Review questions (api, client; ticket LQ-5)

- [ ] 5.1 E2E: Lesson 3 test shows a review question from Lesson 1; answering it wrong does not block the pass.
- [ ] 5.2 Integration: selection uses plan review targets, weakest topic first, at most 2; non-gating; no retry variants or recap; results recorded.
- [ ] 5.3 Client: "Review" label; result not counted in the pass progress.
- [ ] 5.4 Analytics `review_question_answered`.

## 6. Quality evals (engine-eval, evals; ticket LQ-6)

- [ ] 6.1 Judge rubric R1 to R10 in `engine-eval` (one binary call per criterion, evidence quote, family check), thresholds in `evals/thresholds.toml`.
- [ ] 6.2 Calibration anchors: authored example Lessons plus degraded copies; kappa report.
- [ ] 6.3 Planner A/B (D2) on live outline cases; record the evidence and set the `course_plan` route.
- [ ] 6.4 Golden rows (ai-reliability delta) in the Hugging Face dataset; bump `evals/pinned-datasets.toml`; live `mise run engine:eval` passes; re-score the audit's "Generated" column on live samples.
