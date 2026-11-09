## MODIFIED Requirements

### Requirement: CI modes
Draft pull requests SHALL run evals in mock mode with recorded fixtures at no API cost. Ready pull requests that touch `engine/` MUST run a live sample when `ANTHROPIC_API_KEY`, `OPENROUTER_API_KEY`, and `HF_TOKEN` secrets exist, and MUST report `eval_run_completed`. In the absence of required secrets, ready runs SHALL report explicit missing-secret status, retain evaluation reports as 90-day artifacts, and keep the `ci-ok` aggregate passing without fabricating a live pass or enabling feature promotion.

#### Scenario: Draft PR
- **WHEN** a draft pull request runs CI
- **THEN** evals use fixtures, make no network call to providers, and run free of API cost

#### Scenario: Ready PR with missing secrets
- **WHEN** a ready pull request runs without live provider credentials
- **THEN** it reports explicit missing-secret status, generates an unpromoted report artifact, keeps `ci-ok` green, and keeps feature activation disabled

### Requirement: Cost limits
The Engine MUST enforce max input and output tokens per task. The API MUST enforce spend caps from the `credits` spec. A Golden set run MUST have a total budget and stop when it is exceeded.

#### Scenario: Eval budget exceeded
- **WHEN** a live eval run spends more than its budget
- **THEN** the run stops and fails with a budget error

## ADDED Requirements

### Requirement: Live promotion gate
Promotion of new AI models or voices to production SHALL require passing all relevant task thresholds on a pinned Golden set dataset revision during a live evaluation run. Feature activation MUST remain disabled if live evaluation was skipped due to missing secrets or run in mock mode. Evaluation runs MUST immediately reject critical safety, ledger, and cost check failures.

#### Scenario: Feature activation without live pass
- **WHEN** a model change is submitted without passing live evaluation on pinned dataset cases
- **THEN** feature promotion is rejected and production activation remains disabled

#### Scenario: Critical check failure during evaluation
- **WHEN** an evaluation run encounters a critical safety, ledger, or cost check failure
- **THEN** the run fails immediately even if overall score exceeds the task threshold
