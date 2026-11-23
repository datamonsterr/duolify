# Gate AI promotion on reproducible live samples (E10-06)

## Why
Models and voices must not be promoted to production using silent fixture fallbacks when live gate verification is required. At the same time, offline mock runs must remain fast, free, and self-contained for draft pull requests. When ready engine changes run without live provider secrets (such as in local development or before E10-01 credentials are provisioned), the system must report an explicit missing-secret status, keep the CI aggregate passing (`ci-ok`), and keep feature activation disabled without fabricating a live pass. Live runs must enforce task-stratified bounded sampling, strict provider-budget accounting that halts at the evaluation budget, and immediate rejection of critical safety, ledger, and cost violations.

## What Changes
- Add task-stratified bounded sample selection: when an evaluation sample limit is configured, cases are selected across canonical and extended tasks using deterministic round-robin allocation ordered by case ID within each task.
- Add provider-budget accounting: compute input, output, cache-write, and cache-read costs for each evaluation attempt, track cumulative spend across the run, and immediately halt execution if the evaluation budget (`EVAL_BUDGET_USD` or `--budget`) is reached or exceeded.
- Add explicit missing-secret status in ready evaluation mode: when secrets (`ANTHROPIC_API_KEY`, `OPENROUTER_API_KEY`, or `HF_TOKEN`) are absent, log explicit notices, record `missing_secrets` status, keep `ci-ok` CI aggregate passing, and keep feature activation disabled without fabricating a live pass.
- Enforce feature model and voice promotion gate: feature activation requires passing relevant live evaluation cases on pinned dataset revisions; mock fixtures or missing-secret evaluations cannot promote features.
- Reject critical safety, ledger, and cost violations immediately, causing evaluation failure even if non-critical score thresholds are met.
- Extend report contract (both JSON and Markdown) to record cases, revision, provider, usage (tokens and cost), budget, and thresholds, retained as 90-day CI artifacts.
- Preserve draft mock mode as completely offline, deterministic, and free of provider charges.
- Update `docs/data-inventory.md`, `.env.example`, and ADR 0011 defaults.

## Impact
Touches `engine-eval` (runner, main CLI, scoring, report, analytics), eval fixtures, `.github/workflows/_checks.yml`, and documentation. All tests run offline with mock doubles; no real secrets, paid API calls, or external network calls are made.
