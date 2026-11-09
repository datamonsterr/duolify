# Score each AI task with critical checks Design

## Context
ADR 0006 and ADR 0011 specify that prompt and model changes must be evaluated against a Golden set with deterministic assertions and LLM judgment. The current scaffold calculates a single global pass fraction across all evaluated cases. Under this scheme, a catastrophic regression in one task (e.g. `lesson_test`) could be hidden if another high-volume task (e.g. `course_brief_chat`) passes.

## Goals
1. Calculate deterministic and judge scores per canonical task.
2. Allow configuring individual task thresholds in `evals/thresholds.toml`.
3. Support `critical: true` on deterministic checks: failure of a critical check immediately fails the eval run.
4. Bridge legacy route names to canonical task names:
   - `preview` <-> `course_preview`
   - `outline` <-> `course_outline`
   - `lesson` / `slides` <-> `lesson_slides`
   - `test` <-> `lesson_test`
   - `chat` <-> `tutor_chat`
   - `note` <-> `note_enrich`
5. Support extension registration (`variants`, `narration`, `screening`, `steering`).
6. Enforce that canonical tasks have cases in the dataset (missing cases fail the feature gate).
7. Record task, dataset revision, prompt revision, and model in JSON and Markdown reports.
8. Retain eval reports for 90 days as CI artifacts.
9. Emit `eval_run_completed` analytics events with zero learner text and opt-out support.

## Non-Goals
- Live Hugging Face dataset download in offline/mock mode.
- Engine provider routing, fallback, or retry changes (owned by E10-03).
- Proto or API schema modifications.
- Live model promotion (owned by E10-06).

## Decisions
- **Task Aliasing**: The runner accepts cases specified using either canonical names (`course_preview`, `course_outline`, `lesson_slides`, `lesson_test`, `tutor_chat`, `note_enrich`) or aliases (`preview`, `outline`, `lesson`, `chat`, `note`). Reporting and thresholds use canonical names.
- **Immediate Critical Check Failure**: If a check with `critical == true` fails, the case fails and the run is marked failed, regardless of scores meeting thresholds.
- **Mock Mode Zero Network**: Mock mode executes completely offline using recorded fixtures or mock providers.
- **Analytics Delivery**: Emitter respects `ANALYTICS_OPT_OUT=true`, emits per task with `00000000-0000-4000-8000-000000000011`, and never leaks learner text.
