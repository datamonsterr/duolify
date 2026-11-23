# CI and dev deployment

```text
draft PR ── pr-draft ──┐
ready PR / main push / merge_group ── pr-ready ──┤
                      └─ shared checks (tier: fast | full)
                           changes (dorny/paths-filter)
                           ├─ proto lint + generation drift
                           ├─ API lint + unit tests + build
                           ├─ engine lint / build → nextest archive → tests / mock eval
                           ├─ client lint + tests + web build
                           ├─ infra lint
                           └─ workflow lint + OpenSpec validate
                         full also: proto breaking, API integration + HTTP e2e,
                           Playwright, Tauri debug, Docker builds,
                           live sample eval + Railway plan (credentials required)
                         all selected checks → ci-ok
main push → ci-ok → publish GHCR api + engine → successful pr-ready
  → deploy-dev (tested commit, environment: dev)
manual deploy-dev → main only → environment: dev
```

Checks run in parallel by area; PRs select affected areas, while main and merge
groups check every area. Tooling changes select every area. Every job runs on
GitHub-hosted `ubuntu-24.04` (free for this public repository); there is no
self-hosted or third-party runner. Every job and step group has a timeout.

Cheap checks (`repo-lint`, `proto`, `api`, `client-web`, `infra`, `engine-lint`)
are separate jobs listed first, so they start first and fail first. Slow work
(`api-integration-*`, `restore-drill`, `playwright-*`, `tauri`, `images`,
`engine-*`, `client-native`) runs in parallel and never gates them. Inside a job,
steps go lint, then tests, then build, one named step per mise task
(`mise run api:lint`, `mise run api:test`, ...), so the failing step names the task.

In repository Settings → Rules → Rulesets (or branch protection for `main`),
require the GitHub Actions **ci-ok** check, displayed as **checks / ci-ok** for
the reusable workflow. Run CI once to populate the picker. Require no individual
area jobs: unaffected areas are skipped. Only the active draft/ready caller
creates the aggregate. Failed or cancelled dependencies fail it; a failed path
filter cannot turn all skipped jobs into a green check. Enable merge queue only
after the `merge_group` run has passed. Publishing happens after the aggregate
on main; either a failed check or failed publication prevents automatic deploy.

Hosted-runner setup: the client native jobs (`client-native`, `tauri`) run
`mise run ci:tauri-deps`, which installs the
[Tauri 2 prerequisites](https://tauri.app/start/prerequisites/) (WebKitGTK 4.1,
JavaScriptCoreGTK, libsoup 3); the downloaded `.deb` files are cached with
`actions/cache` per runner image version. (`awalsh128/cache-apt-pkgs-action` was
tried and dropped: it saved an empty cache after a 404 from the Ubuntu mirror and
reported success.) Docker and buildx are preinstalled. Rust, Tauri and Docker
jobs run `mise run ci:free-disk` first (hosted runners start with about 14 GB free;
the task removes unused .NET, Android and Haskell toolchains for about 25 GB more).
Playwright system libraries come from `mise run ci:playwright-deps`.

Caches cover mise tools (only each area's required tools), Go modules/builds,
separate engine (`lint`, `build`) and Tauri (`native`, `build`) Rust caches via
`Swatinem/rust-cache`, apt packages, pnpm store, Chromium browsers and
per-image BuildKit `type=gha` scopes. Playwright system libraries are installed
even on browser cache hits. The engine:test workspace suite runs through the
CI-owned `ci:engine-archive` / `ci:engine-test-archive` tasks so test binaries move
between jobs without recompiling. `client:e2e` builds the web app itself (with the
tracer env the tests need), so Playwright shards do not wait for `client-web`.
No raw language test/build commands appear in workflows.

Full-tier API integration runs in three parallel jobs: A covers backup, social and sharing; B covers
notes, narration, lessontest, characters and credits; C covers store, pricing,
accounts, courses, player, eval, gamification and chat. HTTP e2e runs only in C.
The workflow-lint job runs `mise run ci:api-integration-shards` to reject missing
or duplicate packages, differences from the `api:test-integration` default list,
and integration-tagged test directories without shard coverage. Keep each
workflow `API_TEST_PACKAGES` assignment on one line for the guard. `ci-ok` needs
all three jobs directly; `join(needs.*.result, ' ')` passes their results to
`ci:aggregate`, which rejects any failed or cancelled shard.

The same job runs `mise run ci:sql-casts`, which rejects a cast column compared to a
parameter (`col::text = $1`, `= ANY(`, `IN (`) in non-test Go and sqlc sources under
`services/api`: the cast defeats the primary-key and foreign-key indexes. Bind the
parameter instead (`col = $1::uuid`) and validate the id at the handler. A genuinely
text column goes in `scripts/ci/sql-casts.allow` with a reason.

Full-tier Playwright runs as one matrix job with three shards, named
`playwright-1`, `playwright-2` and `playwright-3`. Each sets `E2E_SHARD=N/3`, which
`client:e2e` validates (`N/M`, single digits, N <= M) and passes as
`playwright test --shard=N/3`. Unset runs the full suite. One worker, no retries;
`fullyParallel` is off, so Playwright shards by spec file and each spec runs once.
Artifacts are `playwright-report-1..3`. `ci-ok` needs the matrix job. These
checks replace the former single `checks / playwright` check (#205); branch
protection on `main` lists no required checks by name, so the rename needs no
settings change.

## Full-stack flow suite

`playwright-stack` (full tier, required by `ci-ok`) runs the real web
client against the disposable `infra/e2e` stack: `infra/deploy/compose.yml` plus the
test-only overlay `infra/e2e/compose.yml`, Compose project `duolify-e2e`, mock engine
provider, no `page.route` mocks. Specs are `apps/client/e2e-stack/*.flow.ts`. Each flow
saves `<flow-id>/<nn>-<step>.png` and the run writes `flows.json` (id, title, status,
fixme reason, defects seen on the way, steps with screenshot paths) to
`apps/client/test-results/stack-flows`; CI uploads it as the `stack-flows` artifact.
A flow blocked by a real product gate or defect is reported `fixme` with the reason.

```sh
mise run client:e2e-stack-up      # build images (tag e2e), start on http://localhost:18471
mise run client:e2e-stack         # all flows; E2E_FLOW="F1|F2" for a subset
mise run client:e2e-stack-down    # delete containers and volumes
```

Sign-in codes come from the API's development log mailer (`infra/e2e/stack.sh code
<email>`). Live provider: `OPENROUTER_API_KEY=... E2E_LIVE=1 mise run
client:e2e-stack-up`, then run the suite with `E2E_LIVE=1` (longer timeouts); the key is
passed through the environment only. Remote stack: `E2E_STACK_URL=https://staging.duolify.online
E2E_STACK_CODE_COMMAND='<prints codes for $1>' mise run client:e2e-stack` (no local stack;
`E2E_STACK_GRANT_COMMAND` funds learners, otherwise flows that need paid credits are
`fixme`). `E2E_LESSON_TEST_TRACER_ENABLED=false` runs with lesson generation off.

Environment precedence: mise loads the gitignored `.env` and, by default, `.env` values
replace the caller's environment. `mise.toml` therefore re-applies `OPENROUTER_API_KEY`,
`ELEVENLABS_API_KEY`, `ELEVENLABS_VOICE_ID` and `HF_TOKEN` so a non-empty value exported in
your shell wins over `.env`, and an empty value never overrides a non-empty one (an empty
`OPENROUTER_API_KEY=` copied from `.env.example` no longer blanks a key you export). Add a
new secret to that list in `mise.toml` if it must be overridable from the shell. Tests and
mock evals are hermetic: `engine:test` and `engine:eval` pin `ENGINE_PROVIDER_MODE=mock` and
`EVAL_SAMPLE_LIMIT=0` so a developer's `.env` cannot change their result.

Sign-in codes on a remote stack: the API mails no new code while an unused one was sent in
the last 60 s (cooldown, unchanged), and the 202 response is identical either way. A
consumed code (signing in) never blocks the next request. The harness waits up to 90 s for
real mail (`E2E_STACK_CODE_TIMEOUT_MS` overrides), can accept the still-valid code with
`signInCode(email, { reuse: true })`, and otherwise fails with an error naming the cooldown.

Lesson object metadata: `lessons/` objects are written with an explicit `Content-Type`
(from the key extension) and `Cache-Control: public, max-age=31536000, immutable`.
Objects written earlier keep `application/octet-stream` until rewritten. With the API's
`S3_*` variables set for the target bucket, `mise run api:rewrite-asset-metadata` lists the
objects that need it (dry run) and `mise run api:rewrite-asset-metadata -- -apply`
rewrites them with a self-CopyObject (`MetadataDirective=REPLACE`); it is idempotent. mise lets the
repository `.env` replace the caller's `S3_*`, so from a workstation with a development `.env` the run
uses the local keys and fails with `InvalidAccessKeyId`: run the binary with a clean environment
(`env -i PATH=... S3_ENDPOINT=... S3_REGION=auto S3_BUCKET=... S3_ACCESS_KEY_ID=... S3_SECRET_ACCESS_KEY=... go run ./cmd/rewrite-asset-metadata`, in `services/api`).

Pricing: the suite never approves pricing. Flows that need a quote use the target's
active approved version; a failed course preview or lesson generation fails the flow.

Flows: F1 sign-up/sign-in, F1b new-learner home (check-in toast), F1c session persistence,
F1d daily check-in once per day, F2 create a course, F3 play the first lesson, F4 lesson
test pass, F5 fail then retry variant, F6 tutor, F7 notes and bookmarks, F8 streak/XP/daily
goal, F9 credits, F10 settings, F11 export/delete, F12 friends, F13 free example course (no
AI call, no paid credits), F14 first-run welcome and every tour step, F15 immersive lesson (select a region, ask by text, save the reply as a Quick note; paid AI, so not in the staging list unless the tester pays). Course flows are
funded by the learner's real first daily check-in (keep check-in enabled on the target);
only F9's purchase step uses `grant-credits`. Sign-ups without a deep link dismiss the
welcome and tour as a learner would (`skipOnboarding` in `e2e-stack/stack.ts`).

Staging (`E2E_STACK_URL=https://staging.duolify.online`): run only the flows without paid
AI (`E2E_FLOW="F1 |F1b|F1c|F1d|F9|F10|F11|F12|F13|F14"`). Staging mails real codes through Resend
and has no grant mechanism, so `E2E_STACK_CODE_COMMAND` and `E2E_STACK_GRANT_COMMAND` must
be supplied by the operator (codes: the test inbox in [infra/e2e-mailbox.md](infra/e2e-mailbox.md), `E2E_STACK_EMAIL_DOMAIN=test.duolify.online`); without a grant command F9's purchase step is `fixme`.

| Secret | Use |
| --- | --- |
| `RAILWAY_TOKEN` | Repository secret for full-tier plans; dev environment secret for deploys. Scope to development. |
| `ANTHROPIC_API_KEY` | Unused by the engine since ADR 0015. |
| `OPENROUTER_API_KEY` | Live sample eval primary provider (ADR 0015). |
| `HF_TOKEN` | Live golden-set access (read on `datamonsterr/duolify-golden`). Not set: ready PRs record `missing_secrets`. |
| `POSTHOG_API_KEY` | Optional dev deployment analytics; no analytics key is passed to PR tests. |

Missing secrets never fail CI. `mise run ci:require-secrets` (REQUIRED_SECRETS
lists the names) writes `enabled=false`, a `::notice::` and a job-summary line, and
the steps that need the secret are skipped. The live eval job (`engine-eval-live`, full tier,
engine changes) is the exception: it always runs `mise run engine:eval-live`, which without
`OPENROUTER_API_KEY` and `HF_TOKEN` writes an explicit `missing_secrets` report (uploaded as the
90-day `eval-live-reports` artifact), prints a notice and exits 0, so `ci-ok` stays green and no
live pass is fabricated. With both secrets it runs every case of the pinned revision
(`EVAL_SAMPLE_LIMIT=20`, `EVAL_BUDGET_USD=0.50`, about $0.002 per run), gates the report and
uploads `promotion-candidate.json` when it passes. Optional repository variables override
`HF_GOLDEN_REVISION`, `HF_GOLDEN_DATASET`, `EVAL_SAMPLE_LIMIT` and `EVAL_BUDGET_USD`. The job
also prints `mise run engine:evidence`: whether a deployed engine built from the commit has
valid promotion evidence (see `evals/README.md`, "Live promotion in a deployed engine").
Other skipped steps:
Railway plan, `deploy-dev` (`RAILWAY_TOKEN`) and the scheduled restore drill (the
`BACKUP_*` owner credentials). Offline mock evals still run. GHCR uses the built-in
`GITHUB_TOKEN`, with `packages: write` granted only to the main-only publish job.
Configure the `dev` GitHub environment with main-only deployment rules. Production
is out of scope. `workflow_run` only accepts successful main **push** runs from this
repository, checks out their exact SHA and consumes no upstream artifacts. Deploys
serialize.

Set repository variables `HF_GOLDEN_DATASET` and `HF_GOLDEN_REVISION` for live
evals (defaults match `.env.example`: `datamonsterr/duolify-golden` and `main`).
Pin the revision to an immutable dataset commit for repeatable results.
CI passes `EVAL_SAMPLE_LIMIT=10`; the engine must honor that limit before enabling
live credentials, so a growing dataset does not silently expand PR API costs.

Locally: `mise install actionlint shellcheck`, then `mise run ci:lint`.
OpenSpec uses the verified npm package `@fission-ai/openspec@1.3.1` via mise;
run `mise run ci:openspec` to reproduce specification validation.

## Output and annotations

- Every step is `mise run <task>`; the step name is the task name.
- Annotations: Go and Vitest tests, nextest and Playwright failures are annotated
  from JUnit by `ci:summary` (gotestsum `GOTESTSUM_JUNITFILE`, nextest profile `ci`
  in `engine/.config/nextest.toml`, Vitest and Playwright `junit` reporters when
  `CI` is set; Playwright also uses its `github` reporter, Vitest `github-actions`).
  Problem matchers in `.github/matchers/` (registered by `ci:matchers`) annotate
  tsc, eslint, golangci-lint (paths prefixed with `services/api`), rustc/clippy and actionlint.
- Job summary: `mise run ci:summary` (last step of every job, `if: always()`) writes
  status, duration, test counts and each failing test with `file:line` and the first
  message line to `$GITHUB_STEP_SUMMARY`, and prints the same text in the log.

## For agents

Watch and read failures while CI is still running:

```sh
gh pr checks <n> --watch --fail-fast        # live status; --fail-fast stops at the first failure
mise run ci:failures -- <n>                 # failing jobs, steps, annotations, log tails (PR number or run id)
gh run list --commit "$(gh pr view <n> --json headRefOid -q .headRefOid)"   # run ids for the PR head
gh run view <run-id> --log-failed           # logs of failed steps only
gh run view <run-id> --job <job-id> --log   # one job's full log (job ids: gh run view <run-id> --json jobs)
gh api repos/{owner}/{repo}/check-runs/<job-id>/annotations   # file:line annotations of one job
```

`ci:failures` also works on an in-progress run: a job that has already failed is
reported at once. The job summary is not available through the API, so each job's
`Summary` step repeats it in the log: the failing-test list is the last block of
`gh run view <run-id> --job <job-id> --log`. Find the failing step by name
(`mise run api:test` and so on), then run that same mise task locally.
