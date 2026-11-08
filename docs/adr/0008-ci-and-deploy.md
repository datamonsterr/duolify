# ADR 0008: Two-tier CI on Namespace runners, Railway deploy

- Status: Accepted
- Date: 2026-11-08

## Decision

- **Runners**: `namespace-profile-datamonster01` for every job; `namespace-profile-linux-xl` for heavy jobs (Rust builds, Tauri builds, Docker images). Never `ubuntu-latest`.
- **Draft PR pipeline** (fast): path-filtered lint, unit tests, mock evals, workflow lint. Target under 5 minutes warm.
- **Ready PR / main pipeline** (full): adds integration tests with Postgres and Redis services, Playwright e2e, Docker image builds, Tauri debug build, live sample evals when secrets exist.
- One aggregate `ci-ok` job is the only required status check.
- Caching: mise tool cache, Go module and build cache, `Swatinem/rust-cache`, pnpm store, Docker BuildKit cache (`type=gha`).
- **Deploy**: Railway config as code (`railway.json` per service). Deploys to a `dev` environment only from `main`, after `ci-ok`. Production deploys are manual and out of scope for bootstrap.

## Consequences

- Required check name stays stable as jobs change.
