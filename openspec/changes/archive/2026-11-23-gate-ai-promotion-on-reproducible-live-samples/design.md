# Design: Gate AI promotion on reproducible live samples (E10-06)

## Overview
This design establishes the evaluation and promotion boundary for AI models and narration voices. It ensures that ready changes are evaluated against live models on pinned dataset samples when credentials exist, enforces strict financial budgets during evaluation, handles missing credentials gracefully without fabricating success, and blocks promotion when critical safety, ledger, or cost checks fail.

## Architectural Changes

### 1. Task-Stratified Bounded Sample Selection
When `limit` (via `--limit <N>` or `EVAL_SAMPLE_LIMIT=<N>`) is set:
- Group cases by canonical task.
- Within each task group, sort cases deterministically by `id`.
- Allocate cases across tasks in deterministic round-robin order by sorted task name until the limit is reached or all cases are exhausted.
- Return the stratified cases, preserving deterministic ordering.

### 2. Provider-Budget Accounting & Early Halt
- Model token costs are computed from reported input tokens, output tokens, cache writes, and cache reads using task bounds and nano-rate limits.
- The `Runner` maintains cumulative evaluation cost in nanos/USD.
- If cumulative cost meets or exceeds the configured evaluation budget (`EVAL_BUDGET_USD` or `--budget`):
  - Cease evaluating remaining cases.
  - Set `budget_exceeded = true`.
  - Mark task reports and overall run as failed.
  - Generate evaluation reports indicating the point of termination and budget exhaustion.

### 3. Missing-Secret Handling and CI Aggregation
- In ready pipeline mode (`--ready` or `--mode ready` or `--allow-missing-secrets`):
  - Check presence of `ANTHROPIC_API_KEY`, `OPENROUTER_API_KEY`, and `HF_TOKEN`.
  - If any secret is missing or empty:
    - Emit an explicit notice: `::notice::Live sample evals skipped: credentials unavailable; live promotion disabled`.
    - Generate an evaluation report with `status = "missing_secrets"`, `passed = false`, `live_promoted = false`.
    - Retain reports as 90-day artifacts.
    - Exit with code 0 to allow the CI aggregator (`ci-ok`) to pass without blocking development.
    - Feature activation gates reject promotion without a genuine live pass.

### 4. Critical Violation Rejection
- Critical safety checks (slide HTML validity, content safety, prompt boundary integrity).
- Critical ledger and cost checks (attempt cost exceeding caps, invalid spend reservations).
- If any check with `critical = true` fails, the task and run fail immediately regardless of aggregate score.

### 5. Report Contract (90-day Artifacts)
- Extend `EvalReport` and `CaseResult` with:
  - `provider`: execution provider name.
  - `budget_usd`: configured evaluation budget.
  - `total_cost_usd`: actual cumulative cost in USD.
  - `budget_exceeded`: flag for budget exhaustion.
  - `status`: run status (`passed`, `failed`, `budget_exceeded`, `missing_secrets`, `critical_failure`).
  - `thresholds`: minimum score and per-task thresholds.
  - `usage`: token breakdown (input, output, cache-write, cache-read).
- JSON and Markdown reports written to `evals/reports/` and retained for 90 days.

### 6. Promotion Eligibility
- `is_promotable(report, task)` requires:
  - Live provider execution on a pinned immutable commit hash.
  - Relevant task score >= threshold.
  - Zero critical failures and `budget_exceeded == false`.
  - Status is `passed` (not `missing_secrets` or `fixture`).
