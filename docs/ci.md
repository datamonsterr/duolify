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
groups check every area. Tooling changes select every area. Fast CI targets less
than five minutes with warm caches; measure hosted job summaries before treating
that as achieved. Every runner job has a timeout. The two caller jobs delegate
runner selection and timeouts to `_checks.yml`.

In repository Settings → Rules → Rulesets (or branch protection for `main`),
require the GitHub Actions **ci-ok** check, displayed as **checks / ci-ok** for
the reusable workflow. Run CI once to populate the picker. Require no individual
area jobs: unaffected areas are skipped. Only the active draft/ready caller
creates the aggregate. Failed or cancelled dependencies fail it; a failed path
filter cannot turn all skipped jobs into a green check. Enable merge queue only
after the `merge_group` run has passed. Publishing happens after the aggregate
on main; either a failed check or failed publication prevents automatic deploy.

Configure Namespace GitHub integration and the profiles
`namespace-profile-datamonster01` and `namespace-profile-linux-xl`. Use Linux
images with a current Actions runner, Docker daemon/buildx support and passwordless
sudo/apt. Rust engine work, client Rust checks, Tauri, Playwright and Docker builds
use `linux-xl`. The client setup installs the [Tauri 2 prerequisites](https://tauri.app/start/prerequisites/),
including WebKitGTK 4.1 and the explicit JavaScriptCoreGTK/libsoup development
packages. Prebake these packages into the profiles for warm-run speed.

Caches cover mise tools (only each area's required tools), Go modules/builds,
separate engine and Tauri Rust workspaces, pnpm store, Chromium browsers and
per-image BuildKit `type=gha` scopes. Playwright system libraries are installed
even on browser cache hits. The engine:test workspace suite runs through the
CI-owned `ci:engine-archive` / `ci:engine-test-archive` tasks so test binaries move
between jobs without recompiling. Web build artifacts are reused by Playwright.
No raw language test/build commands appear in workflows.

| Secret | Use |
| --- | --- |
| `RAILWAY_TOKEN` | Repository secret for full-tier plans; dev environment secret for deploys. Scope to development. |
| `ANTHROPIC_API_KEY` | Live sample eval primary provider. |
| `OPENROUTER_API_KEY` | Live sample eval fallback provider. |
| `HF_TOKEN` | Live golden-set access. |
| `POSTHOG_API_KEY` | Optional dev deployment analytics; no analytics key is passed to PR tests. |

Live evals require all three eval secrets. Missing eval/plan credentials or fork
PRs produce a notice and skip those steps; offline mock evals still run. A missing
dev Railway token fails deployment. GHCR uses the built-in `GITHUB_TOKEN`, with
`packages: write` granted only to the main-only publish job. Configure the `dev`
GitHub environment with main-only deployment rules. Production is out of scope.
`workflow_run` only accepts successful main **push** runs from this repository,
checks out their exact SHA and consumes no upstream artifacts. Deploys serialize.

Set repository variables `HF_GOLDEN_DATASET` and `HF_GOLDEN_REVISION` for live
evals (defaults match `.env.example`: `datamonsterr/duolify-golden` and `main`).
Pin the revision to an immutable dataset commit for repeatable results.
CI passes `EVAL_SAMPLE_LIMIT=10`; the engine must honor that limit before enabling
live credentials, so a growing dataset does not silently expand PR API costs.

Locally: `mise install actionlint shellcheck`, then `mise run ci:lint`.
OpenSpec uses the verified npm package `@fission-ai/openspec@1.3.1` via mise;
run `mise run ci:openspec` to reproduce specification validation.
