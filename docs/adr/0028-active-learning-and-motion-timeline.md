# ADR 0028: Active learning interactions, successive relearning and a declarative motion timeline

- Status: Accepted
- Date: 2026-11-24
- Change: `openspec/changes/knowledge-retention`

## Context

Lessons are passive: narrated Slides, then a multiple-choice test. The strongest retention techniques are active: retrieval practice and successive relearning, generation and pretesting, self-explanation, free recall, and confidence with hypercorrection. Unguarded AI help can harm exam performance (Bastani et al. 2025). Slide motion is freehand CSS written by a cheap model. Remotion (paid company license, model-written React) and GSAP (license bars visual-builder products) were assessed and rejected for runtime use.

## Decision

- Interactions (Guess first, Explain why, Free recall, confidence tap, Teach it back) are native app cards at Slide checkpoints. They are never inside the sandbox, always skippable, and never gating.
- Free text is graded by a cheap rubric grader that fails open to a self-check list. It is platform-paid and rate-capped.
- A per-topic successive-relearning schedule (1, 3, 7, 21 days; retire after 3 sessions) drives a daily Review session from stored variants. Review uses no generation and no Credits, and counts for the Streak.
- The tutor uses a hint ladder with a deterministic answer guard.
- Slides may carry a declarative `timeline` (closed effect enum, id targets, narration cues). A trusted runtime of about 3 KB compiles it to Web Animations API animations that the existing bridge controls. The model never writes JavaScript.

## Consequences

- More engagement surface per Lesson (about 1 minute longer). Success is measured on 7-day and 30-day Review accuracy, not completion.
- New platform-paid grading cost, about $0.00005 per grade, capped.
- `topic_schedules` adds Learner progress data to backups and erasure.
- Motion stays swappable: the JSON contract can move to Motion `animate()` later without changing generation.
