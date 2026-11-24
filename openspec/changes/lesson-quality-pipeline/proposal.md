## Why

Owner request (2026-11-24): optimise the course creation and lesson generation pipeline for learning quality, not only format and safety. An audit (`design.md` section 1) scored the generated path at about 7/20 on a learning-science rubric against 15/20 for the authored example Courses. Main causes:

- The lesson prompt (`lessontest::LESSON_SHAPE`, `regions::shape`) is all format and safety rules. It asks for nothing about lesson structure: no single objective, no hook or recall, no worked example, no misconception-based distractors, no on-screen word cap, no narration-versus-screen rule.
- The paid `GenerateCourseOutline` call asks the model to echo the confirmed outline byte for byte and then discards the answer (`engine-server` returns `base.clone()`). The Learner pays the `outline` action for no output.
- The Course brief's `lesson_length` (short, medium, long) never reaches lesson generation; every Lesson is 2 to 4 Slides.
- Every lesson eval is a deterministic schema check. The judge score is null for every task in `evals/promotion/live-report.json`, so learning quality is not measured.
- No spaced retrieval across Lessons: earlier topics never come back.
- The narration validator accepts 6 words per second (360 wpm) while the prompt asks for at most 150 wpm.
- The lesson context is the Rust `Debug` dump of the whole request, including usage limits.

## What Changes

- **Course plan.** The paid outline step becomes a planner call. It keeps every confirmed Unit and Lesson title, goal and order unchanged, and adds one Lesson plan per Lesson: a measurable objective (observable verb), 2 to 4 key points, prerequisite Lesson ids, 1 to 3 likely misconceptions, an example idea and review targets (earlier Lessons to recall). The planner runs on a stronger route, chosen once by an eval A/B. It runs once per Course, so per-Course cost stays in cents.
- **Outline clarification.** The Course preview returns 0 to 3 clarifying questions when the outline depends on something the brief does not settle (prior knowledge of a prerequisite, the end project, depth on an ambiguous topic). Each question has quick-reply options. An answer goes through the existing quoted outline-proposal flow (the Learner accepts or rejects a diff), and all answers go to the planner as data. A new prompt skill `outline_clarify` carries the rules.
- **Lesson structure core.** The always-on lesson core gains a teaching contract: one objective from the Lesson plan; Slide 1 recalls a prerequisite or opens with a hook question; concept, then worked example, then a check; a closing summary; wrong choices are named misconceptions, each with an explanation; at most 40 visible words per Slide; narration explains and does not read the Slide. It applies in both `monolithic` and `routed` prompt modes.
- **Lesson length per Course.** The brief's `short`, `medium` and `long` map to Slide and question targets (3/2, 5/3, 8/5 Slides/questions, within the current `lesson_content` envelope). The value is stored on the Course, editable in Course settings, and sent to `GenerateLesson`.
- **Credit estimate for one Lesson.** Preview, outline confirmation and Course settings show the estimated Credits for one Lesson of this Course at its length (lesson content plus narration when on). Pricing config gains an optional per-length `lesson_content` price. It defaults to today's single price, so this change raises no price. Any tiered price goes through the existing owner-approved pricing workflow.
- **Review questions.** The API adds up to 2 stored question variants from earlier Lessons' review targets (weakest topics first) to each Lesson test as non-gating review questions. This needs no new generation and no Credits.
- **Quality evals.** Static quality checks run in `validate_lesson`: visible words per Slide, narration pace, narration-to-screen overlap and the Lesson-plan objective being taught. An offline LLM judge scores a 10-criterion rubric, one binary call per criterion, on a model family different from the generator. It is calibrated on the authored example Courses (positive anchors) and degraded copies (negative anchors). The `lesson_slides` golden set grows to at least 30 cases.
- **Compact context.** The lesson request is serialised as compact JSON with only the outline, current Lesson and plan, Learner, length, the last 5 history summaries and weak topics. Usage limits and IDs not needed for generation are left out.
- Narration pace bound aligned to 3.3 words per second (about 200 wpm).

## Capabilities

### Modified Capabilities

- `course-creation`: Course plan, outline clarification, lesson length, Lesson credit estimate, lesson structure and context.
- `lesson-test`: non-gating review questions.
- `ai-reliability`: quality rubric, static quality checks, LLM judge calibration, planner route A/B, golden rows.
- `pricing-config`: optional per-length lesson price.
- `analytics`: new events and properties.

## Impact

- `proto`: `LessonPlan` on `LessonRef`; `lesson_length`, `review_targets` on `GenerateLessonRequest`; `clarifying_questions` on the preview response; `clarifications` on the outline request. Matching `services/api` and `engine` changes in one PR (AGENTS.md rule).
- `engine`: planner prompt and validator, lesson core text, skills `outline_clarify` (course_preview) and `lesson_length` rule, static checks, context builder, narration bound, judge rubric in `engine-eval`, a new `course_plan` route in `models.toml` and prices in `prices.toml`.
- `services/api`: migration for `lesson_plans` (jsonb on lesson stubs) and `courses.lesson_length`, estimate endpoint, review-question selection, clarification storage on the draft.
- `apps/client`: clarifying questions in the outline review chat, length control in Course settings, estimate chip.
- Depends on PR #427 (prompt skills for `course_preview`). PR #408 (AI Gateway) touches the provider layer only; no conflict expected.
- Data: Lesson plans and clarification answers are Course data. They follow existing Course backup, export and erasure rules.
