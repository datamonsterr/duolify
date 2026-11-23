# Full-stack user-flow e2e with reviewed screenshots

## Why
The Playwright suite (`apps/client/e2e`) never runs the real product: it serves `pnpm preview`
against a Go test server with a fixture engine, and many specs mock our API with `page.route`.
No screenshots are kept. The owner's definition of done is that every user flow passes against
the real running app, with per-flow screenshots the owner reviews.

## What Changes
- New Playwright project `apps/client/playwright.stack.config.ts` with flow specs in
  `apps/client/e2e-stack/*.flow.ts`. It drives the real web client on a running stack; the
  base URL comes from `E2E_STACK_URL` (local compose stack or a remote URL such as staging).
- Disposable local stack: `infra/deploy/compose.yml` plus a test-only overlay
  `infra/e2e/compose.yml`, run by `infra/e2e/stack.sh` as Compose project `duolify-e2e`.
- Engine runs `ENGINE_PROVIDER_MODE=mock` by default (deterministic, free); `E2E_LIVE=1`
  switches to the real OpenRouter route.
- Each flow saves named full-page screenshots to `<flow-id>/<nn>-<step>.png` and the run
  writes `flows.json` (flow id, title, steps, screenshot paths, outcome).
- mise tasks `client:e2e-stack-up`, `client:e2e-stack`, `client:e2e-stack-down`; a full-tier CI
  job runs the deterministic suite and uploads the screenshots.
- First flows: F1 sign-up and sign-in, F2 create a course, F3 play the first lesson, F4 lesson
  test pass path. Flows blocked by a real product gate are `test.fixme` with the reason.

## Impact
Client test tooling, `infra/e2e` (new), mise tasks, CI workflow, docs. No product code,
no `proto` change, no change to the production compose file.
