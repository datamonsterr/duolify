# ADR 0027: Course planner, teaching core and lesson quality evals

- Status: Accepted
- Date: 2026-11-24
- Change: `openspec/changes/lesson-quality-pipeline`

## Context

An audit (change `design.md` section 1) scored generated Lessons at about 7/20 on a learning-science rubric, against 15/20 for the authored example Courses. The lesson prompt has format and safety rules only. The paid outline call echoes the confirmed outline and its answer is thrown away. The brief's lesson length is ignored. Lesson evals check the schema only, with no judge.

## Decision

- The paid outline step becomes a **Course planner**. It keeps the confirmed outline unchanged and adds one immutable Lesson plan per Lesson: objective, key points, prerequisites, misconceptions, example idea and review targets. It runs once per Course on a `course_plan` route chosen by an eval A/B (Sonnet 5.5, Haiku 5.5 or Hy4 Flash). The cheapest candidate within 0.05 of the best judge score wins.
- Per-Lesson generation stays on the cheap route. It gets the plan and an always-on **teaching core**: one objective, recall or hook first, worked example before the check, summary last, misconception distractors, at most 40 visible words per Slide, and narration that explains rather than reads the Slide.
- **Lesson length** (short, medium or long) is stored per Course and drives the Slide and question counts. A **Credit estimate for one Lesson** is shown wherever length is chosen. Per-length prices are optional and default to today's single price.
- The preview may ask up to 3 **clarifying questions**. Answers change the outline only through the existing quoted proposal flow.
- **Review questions** come from stored variants of earlier Lessons. They are non-gating, and no generation is involved.
- Quality is gated offline by static checks and an LLM judge. The judge makes one binary call per criterion, uses a different model family from the generator, and is calibrated at kappa of at least 0.6 on owner-labelled anchors. In production, static checks only warn.

## Consequences

- The Learner's outline payment now buys a real artifact, and the cheap per-Lesson model writes against a plan.
- Per-Course cost may rise by up to about $0.05 if Sonnet wins the A/B, with no change to per-Lesson cost.
- Engine prompts change, so the promotion fingerprint and live evidence must be refreshed before rollout.
- The proto changes twice (plan, then length), each with matching API and engine changes.
- Clarifying questions depend on prompt skills for `course_preview` (PR #427).
