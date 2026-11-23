## Why

Duolify has foundation docs and ADRs but no code. E0 creates the monorepo scaffolds, tooling, CI and infra so that product epics E1 to E11 can start in parallel without rework.

## What Changes

- Scaffold `services/api` (Go), `engine` (Rust), `apps/client` (Tauri 2 + React), `proto` (buf), `infra` (docker compose, Railway config), `evals`.
- Add mise tasks per area: `<area>:lint`, `<area>:test`, `<area>:build`.
- Add draft and ready CI pipelines with one `ci-ok` check (ADR 0008).
- Initialize OpenSpec and write the first product specs (13 capabilities).
- Define the backup stub and the eval harness skeleton (mock mode first).
- Out of scope: real auth, Stripe, production deploy, real eval dataset content.

Work packages: WP-API, WP-ENGINE, WP-CLIENT, WP-PROTO, WP-SPECS, WP-INFRA, WP-CI, WP-DOCS (see `docs/epics/README.md`).

## Capabilities

### New Capabilities
- `repo-tooling`: mise tasks, area layout and CI gates every area must expose.

### Modified Capabilities

## Impact

- Areas: all (client, api, engine, proto, infra, evals, ci).
- ADRs relied on: [0001](../../../docs/adr/0001-monorepo-layout.md), [0002](../../../docs/adr/0002-service-architecture.md), [0003](../../../docs/adr/0003-data-stores.md), [0004](../../../docs/adr/0004-auth.md), [0005](../../../docs/adr/0005-client-and-lesson-player.md), [0006](../../../docs/adr/0006-llm-providers-and-evals.md), [0007](../../../docs/adr/0007-credits-and-payments.md), [0008](../../../docs/adr/0008-ci-and-deploy.md).
- Product specs for E1 to E11 live in `openspec/specs/`; this change does not alter them.
