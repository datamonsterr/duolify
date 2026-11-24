Each group is one ticket and one PR. TDD order: e2e, integration, unit, code. Depends on `lesson-quality-pipeline` (Lesson plans, teaching core).

## 1. Motion timeline (proto, engine, client; KR-1)

- [ ] 1.1 E2E: a fixture Lesson with a `draw` and a cue-timed `highlight` plays, seeks back, and honours reduced motion.
- [ ] 1.2 Proto `Slide.timeline`; engine schema and validator (D8); prompt rule in the teaching core: prefer timeline steps over handwritten `@keyframes`.
- [ ] 1.3 Client `lesson-player/timeline.ts` runtime (WAAPI, `fill: both`), injected like `motionBridge`; cue resolution from alignment.
- [ ] 1.4 Golden rows; `engine:test`, `engine:eval`, `client:test`.

## 2. Interaction cards and grader (proto, engine, api, client; KR-2)

- [ ] 2.1 E2E: Guess first, Explain why (text and voice), Free recall, Skip, grader failure, offline.
- [ ] 2.2 Proto Lesson `interactions`; engine generation in the lesson core; `GradeFreeText` RPC with strict schema and repair.
- [ ] 2.3 API grade endpoint: platform-paid, 3 per Lesson, 30 per day, free-text retention of 30 days; checkpoint wiring in the manifest.
- [ ] 2.4 Client cards at checkpoints, outside the frame; voice input reuse.
- [ ] 2.5 Grader golden rows (ai-reliability delta); analytics.

## 3. Confidence tap, topic schedule and daily Review (api, client; KR-3)

- [ ] 3.1 E2E: pass creates schedules; Review next day mixes Courses; confident miss is due the next day; Review keeps the Streak.
- [ ] 3.2 Migration `topic_schedules`; scheduler (D4); Review endpoints from stored variants; confidence on attempts.
- [ ] 3.3 Client Review entry on Home, session screen, confidence tap on test, retry and review.
- [ ] 3.4 Backup, export and erasure; analytics.

## 4. Tutor hint ladder, answer guard and teach-back (engine, api, client; KR-4)

- [ ] 4.1 E2E: asking for the answer gets a hint; teach-back summary names a missed point.
- [ ] 4.2 Engine tutor policy, deterministic answer check, regenerate then template; teach-back prompt using persona style only.
- [ ] 4.3 API context: unanswered question ids and correct texts for the guard (never sent to the client).
- [ ] 4.4 Client teach-back entry at the Lesson end; analytics; tutor golden rows.

## 5. Teaching core additions (engine; KR-5)

- [ ] 5.1 Concreteness fading, one analogy with "where it breaks", and a "rests on: A, B" line in the lesson core (about 60 tokens). Judge criteria added to the LQ-6 rubric.
