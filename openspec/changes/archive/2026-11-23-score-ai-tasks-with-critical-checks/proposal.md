# Score each AI task with critical checks (E10-02)

## Why
The previous evaluation scaffold used a global aggregate PASS fraction across all cases, which could allow a task with a failing critical check or a completely failing route to pass if other tasks had enough passing cases. Furthermore, canonical spec task names were not aligned with legacy router task aliases.

## What Changes
- Upgrade scoring from a single global PASS fraction to per-task deterministic and judge scores.
- Support per-task thresholds in `evals/thresholds.toml`.
- Add critical-check support: failure of any deterministic check marked `critical` causes immediate failure of the run.
- Bridge existing routes (`preview`, `outline`, `lesson`, `chat`, `note`) to canonical task names (`course_preview`, `course_outline`, `lesson_slides`, `lesson_test`, `tutor_chat`, `note_enrich`) via explicit task aliases.
- Provide task dispatch and extension registration (`variants`, `narration`, `screening`, `steering`), where missing required task cases fail the feature gate.
- Record task, dataset revision, prompt revision, and model in JSON and Markdown eval reports, retained for 90 days as CI artifacts.
- Emit `eval_run_completed` (task, score, threshold, passed) with the reserved system UUID, common properties, opt-out support, and zero learner text.
- Add golden fixtures covering per-task thresholds, critical failures, and rubric calibration.

## Impact
Touches `engine-eval`, eval fixtures, reports, and data documentation. No proto or services/api changes except the analytics allowlist. Mock mode requires zero network calls.
