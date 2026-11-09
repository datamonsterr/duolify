## Why

Lessons are generated one at a time (Gated generation), so a learner can say what they want next: "more examples in Go", "skip history". Today nothing carries that to the Engine. Learners who cannot steer either abandon the course or restart it. Steering must also be safe: free text goes into an LLM prompt, so it is a prompt-injection surface, and it must not break grading, pricing or published-course reuse (E15).

## What Changes

- New capability `course-steering`: add, list, edit and delete **Course instructions** with scope `next lesson` or `rest of course`; instructions shape future lesson generation only; optional **re-plan remaining outline** with diff-before-confirm and a credit charge from the **Pricing config**.
- Steering a **Course copy** of a **Published course** forks future generation to the learner (reuse of published lessons stops from that point).
- Safety: screening of instructions, bounded data-only prompt section, limits, golden-set cases for adherence and injection resistance.
- Deltas: `course-creation` (generation input includes active instructions; re-plan credits), `lesson-test` (steering never changes grading or pass rules; past tests unchanged).
- Proto additions are proposed in `design.md` only. No proto or code change here.

## Capabilities

### New Capabilities

- `course-steering`: instructions, effect rules, re-plan, fork, safety, limits, analytics, backup, model reliability.

### Modified Capabilities

- `course-creation`: gated generation receives active Course instructions.
- `lesson-test`: steering cannot weaken the Lesson test.

## Impact

- Client: instruction panel in the course screen (add, list, edit, delete), fork warning, re-plan diff view and confirm, quote display.
- API: instruction store, screening call, scope/consumption logic, fork bookkeeping on Course copy, re-plan proposal store and apply, credit reserve/settle, rate limits.
- Engine: prompt section for instructions, `ReplanOutline`, instruction screening, evals.
- Proto: proposed `CourseInstruction`, `instructions` on `GenerateLessonRequest`, `ReplanOutline` RPC, `ScreenInstruction` RPC (design.md). Needs matching API and Engine changes in the implementation PR.
- Infra: Postgres tables in nightly dump; no new services.
- ADRs relied on: [0002](../../../docs/adr/0002-service-architecture.md), [0006](../../../docs/adr/0006-llm-providers-and-evals.md), [0007](../../../docs/adr/0007-credits-and-payments.md).

## Dependencies and scope

- Needs E4 (gated generation, lesson tests) and E7 (credits). Re-plan price comes from `pricing-config`; the key `outline_replan` is not in the approved E12 table and must be approved as a new Pricing config version before re-plan launches (open question).
- The fork behavior depends on Course copy semantics in `course-sharing-marketplace`; this change is valid for private Courses without it.
- Out of scope: rewriting or regenerating already generated lessons, shared or social instructions, instruction templates, voice input.
