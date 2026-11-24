## Why

Owner request (2026-11-24): make knowledge stick, using first-principles, Socratic and Feynman-style learning, and richer motion in lesson Slides.

Lessons today are passive. The Learner watches 2 to 4 narrated Slides, then takes a multiple-choice test. Nothing asks them to generate, explain or recall, and the strongest retention techniques (Dunlosky et al. 2013; Roediger & Karpicke 2006; Rawson & Dunlosky 2011) are all active. Fluent narration without responses creates an illusion of learning (Chi & Wylie 2014, ICAP). Slide motion is limited to CSS the cheap model writes freehand, so diagrams cannot build step by step in sync with narration.

This change builds on `lesson-quality-pipeline` (PR #434): Lesson plans, the teaching core and review questions.

## What Changes

- **Guess first.** Before the concept Slide, the player pauses at a checkpoint and shows one native "Guess first" question card (tap, no penalty, not gating), then reveals. Generation effect and pretesting (Slamecka & Graf 1978; Richland, Kornell & Kao 2009).
- **Explain why.** After the worked example, a native card asks one "why does this step work?" question. The Learner types or speaks (existing voice input), or skips. A cheap grader checks it against 2 to 3 key ideas written at generation time and shows the model answer with the ideas covered and missed. This covers self-explanation and elaborative interrogation (Chi et al. 1994; Bisra et al. 2018). The **Feynman technique** maps here and to teach-back; its evidence is the techniques it bundles, so the product does not claim "Feynman" effects.
- **Free recall.** The Lesson ends with "Write what you remember". It shows a coverage check against the Lesson's key points (Karpicke & Blunt 2011). Skippable and not gating.
- **Confidence tap.** Test and review questions take a one-tap "Sure / Think so / Guess" before the answer. A confident miss gets a highlighted correction and comes back sooner (hypercorrection: Butterfield & Metcalfe 2001).
- **Successive relearning and daily Review.** Each taught topic gets a schedule: due after 1, 3, 7 and 21 days; a miss resets to 1 day; retired after correct recall in 3 separate sessions. A daily **Review** session (counts for the Streak) mixes due topics across Lessons and Courses (interleaving: Rohrer et al. 2020), reusing stored question variants. No generation and no Credits.
- **Socratic tutor guardrail and teach-back.** Tutor chat on a Lesson topic uses a hint ladder (nudge, then sub-question, then worked step) and never states the answer to a graded or review question (Bastani et al. 2025: unrestricted AI help raised practice scores but lowered exam scores by 17%). An optional "Teach it back" mode: the course Character plays a confused novice, asks at most 2 naive follow-ups, then lists what was right and what was missing (learning by teaching: Fiorella & Mayer 2013).
- **First principles and concreteness fading in the teaching core.** Each concept goes concrete example, then diagram, then rule (Fyfe et al. 2014). One analogy with a "where it breaks" line, and one "this rests on: A, B" line naming the 2 or 3 facts the idea depends on.
- **Motion timeline.** Each Slide may carry a declarative `timeline` (at most 24 steps): `{at, target, effect, dur}`. `at` is milliseconds or a narration cue (a word index resolved through the TTS alignment). `effect` is a closed enum: `reveal`, `fade_out`, `slide_in`, `highlight`, `underline`, `draw` (SVG stroke), `pulse`, `count_up`, `typewriter`, `move_to`, `dim_others`. `target` is an element id. A trusted runtime bundled in the Slide frame compiles the timeline to Web Animations API animations, which the existing bridge already seeks, pauses and rate-controls. The model writes no JavaScript, and Slide scripts stay banned by the prompt.

## Capabilities

### New Capabilities

- `retention-review`: topic schedule, daily Review session, confidence-based rescheduling.

### Modified Capabilities

- `lesson-player`: checkpoint cards (Guess first, Explain why, Free recall) and the motion timeline runtime.
- `lesson-test`: confidence tap.
- `tutor-chat`: hint ladder, answer guard, teach-back mode.
- `ai-reliability`: grader and timeline golden rows, grader fallback.
- `analytics`: new events.

## Impact

- `proto`: Slide `timeline` and `interactions` (guess, why prompt with key ideas, recall key points); grading RPC `GradeFreeText`.
- `engine`: teaching-core additions, timeline schema and validator, interaction generation, grader (Hy4 Flash, strict schema, platform-paid, at most 3 per Lesson), tutor policy.
- `services/api`: `topic_schedules` table, Review session endpoints, confidence on attempts, grading endpoint with rate limits.
- `apps/client`: timeline runtime (`lesson-player/timeline.ts`, injected like `motionBridge`), interaction cards, Review entry on Home, confidence tap, teach-back mode.
- Credits: grading and teach-back turns are platform-paid inside the protected cost ceiling (about $0.00005 per grade on Hy4 Flash). Review is free. No price change.
- Out of scope: Remotion, GSAP and video rendering; Lottie or Rive asset library (a later change); productive-failure challenges; mnemonics.
