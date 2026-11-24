# Design: knowledge-retention

## 1. Research summary (2026-11-24)

### Motion tools

| Tool | Live in browser | License or cost | Model-writable safely | Verdict |
|---|---|---|---|---|
| Web Animations API (WAAPI) and CSS | yes, every WebView | free | as data, through our runtime | **Use.** The bridge already seeks `document.getAnimations()` |
| Motion (`animate()`, motion.dev) | yes | MIT; free `/motion` agent skill | as data, through a runtime | Optional later (springs, stagger); ADR 0005 already allows a pinned copy |
| GSAP | yes | free since 2025-04, but the license bars "visual animation builder" products and Webflow can amend it | as data | Not now; legal grey area, larger bundle |
| Remotion | Player plays live | company license (Automators, $100/mo minimum); Player use counts | needs model-written React | **No.** Video-oriented, paid, needs code |
| Lottie and dotLottie, Rive | yes | MIT runtimes | not authorable by the model | Later: a curated asset library triggered by timeline cues |
| Motion Canvas, Revideo, Manim | render-oriented | MIT | model writes code | No runtime use; Manim maybe for offline math SVG assets |
| HyperFrames (HeyGen) | renders to MP4 | Apache-2.0 | HTML plus GSAP | Prior art for prompting only |

Sources: remotion.dev/docs/license/faq.md, remotion.dev/docs/ai/skills, motion.dev/ai-kit, gsap.com/community/standard-license, webflow.com/blog/gsap-becomes-free, lottiefiles.com/dotlottie, github.com/heygen-com/hyperframes, webkit.org/blog/17333. The GSAP and Remotion license readings are from their docs as of 2026-11-24, not legal advice.

**Decision M1:** ship a timeline runtime of about 3 KB built on WAAPI only, with no library. The JSON contract keeps the engine swappable: Motion `animate()` can replace it if springs or stagger are needed.

### Learning techniques (evidence: S strong, M moderate, W weak)

| Technique | Evidence | Mechanic here |
|---|---|---|
| Retrieval practice plus successive relearning | S (Roediger & Karpicke 2006; Rawson & Dunlosky 2011) | topic schedule and daily Review |
| Generation effect and pretesting | S and M (Slamecka & Graf 1978; Richland et al. 2009) | Guess first card |
| Self-explanation and elaborative interrogation | S and M (Chi et al. 1994; Bisra et al. 2018; Dunlosky et al. 2013) | Explain why card |
| Free recall | S (Karpicke & Blunt 2011) | Free recall card |
| Confidence and hypercorrection | M (Butterfield & Metcalfe 2001) | confidence tap |
| Interleaving | M, mainly for confusable categories (Brunmair & Richter 2019) | mixed Review |
| Learning by teaching | M (Fiorella & Mayer 2013) | Teach it back |
| Socratic AI tutoring | M (Kestin et al. 2025, Sci Rep, doi:10.1038/s41598-025-97652-6); harm without guardrails (Bastani et al. 2025, PNAS, doi:10.1073/pnas.2422633122) | hint ladder and answer guard |
| Concreteness fading | M (Fyfe et al. 2014) | teaching core |
| Feynman technique | W as a named method | covered by self-explanation, recall and teach-back |
| First-principles decomposition | W as named; close to subgoal labels | "rests on" line plus tutor prompt |
| Learning styles | debunked (Pashler et al. 2008) | not built |

Only Kestin 2025 and Bastani 2025 were checked this session, from search results; spot-check the other DOIs before citing them outside the repo.

## 2. Decisions

| # | Decision |
|---|---|
| D1 | Interaction cards are native app UI shown at Slide checkpoints. They are never inside the sandboxed Slide, so input never touches model HTML. |
| D2 | All new interactions are skippable and never gate a pass. Only the existing Lesson test gates. |
| D3 | Free-text grading: Hy4 Flash, temperature 0, strict schema `{ideas:[{id, status: present|partial|absent}], misconception: bool}`, Learner text delimited as data, lenient (when unsure, `partial`). One repair attempt; on failure show the model answer as a self-check list. Platform-paid; at most 3 grades per Lesson and 30 per Learner per day. Grades feed feedback and scheduling only. |
| D4 | Schedule per (Learner, topic_id): steps 1, 3, 7, 21 days; a wrong answer resets to step 1; a confident miss is due the next day; retired after 3 sessions with first-try correct recall. Review takes at most 10 questions, due topics first, at most 2 per topic, mixed across Courses, from stored variants only. |
| D5 | Review counts for the Streak like a Lesson (gamification rule unchanged otherwise). |
| D6 | Tutor answer guard: when the chat context includes a graded or review question the Learner has not answered yet, the tutor gives hints only. The guard is a prompt rule plus a deterministic check that the reply does not contain the correct choice text; on a hit, the reply is regenerated once, then replaced with a hint template. Socratic loops are capped at 3 turns, then a worked step. |
| D7 | Teach it back: optional at the Lesson end, at most 2 Character questions, then a rubric summary from the grader. It uses the Cast persona style only (existing persona boundary). Platform-paid, at most 1 per Lesson. |
| D8 | Timeline contract: at most 24 steps; targets must be ids present in the Slide; `dur` 100 to 4000 ms; `at` within `duration_ms`, or `{cue: word_index}` resolved by the player from alignment (fallback: proportional position in the narration text). `prefers-reduced-motion` turns every effect into an instant end state. |
| D9 | The runtime is app code injected before the Slide markup, like `motionBridge`. It creates WAAPI animations with `fill: both` so the bridge's seek and pause stay authoritative. |

## 3. Data

- `topic_schedules` (learner_id, course_id, topic_id, step, due_at, successes, retired_at). Learner progress data: nightly backup, export, erasure cascade.
- Interaction answers: grades kept 90 days (enums only); Learner free text kept with the Lesson attempt for 30 days, then deleted; never sent to PostHog.
- Timeline and interactions are Slide manifest data, versioned with the Lesson.

## 4. Analytics

`guess_first_answered` {lesson_id, correct, skipped}, `explain_why_submitted` {lesson_id, coverage, skipped, input: text|voice}, `free_recall_submitted` {lesson_id, coverage, skipped}, `confidence_tapped` {question_kind, level, correct}, `review_started` and `review_completed` {due_count, answered, correct}, `teach_back_completed` {lesson_id, coverage}, `tutor_answer_guard_hit` {lesson_id}. No learner text.

## 5. Rollout

Flags: `interaction_cards`, `retention_review`, `confidence_tap`, `tutor_hint_ladder`, `teach_back`, `motion_timeline`. A/B the 7-day and 30-day Review accuracy of Learners with versus without interaction cards. That is the success metric, not completion rate.
