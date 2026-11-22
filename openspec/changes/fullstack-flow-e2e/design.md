# Design

## Stack
`infra/e2e/stack.sh` merges `infra/deploy/compose.yml` with `infra/e2e/compose.yml` under its
own project name, env file (`infra/e2e/.state/.env`, git-ignored, secrets from
`selfhost.sh init`) and image tag `e2e`, so it never touches a running `duolify-selfhost`
stack. `down` deletes the volumes: every run starts empty. Like `selfhost.sh`, Compose sees an
allowlisted environment only; in live mode `OPENROUTER_API_KEY` is passed through the process
environment and never written to disk.

The overlay changes test plumbing only: sign-in throttles are raised (all requests come from
one IP). `LESSON_TEST_TRACER_ENABLED` stays as in production (unset, so off) unless
`E2E_LESSON_TEST_TRACER_ENABLED=true` is exported to probe the gated flows.

## Sign-in codes
`DUOLIFY_ENV=development` selects the API's `LogMailer`, which logs each code as
`DEV ONLY sign-in mail` JSON. `stack.sh code <email>` reads it from the API log. No mail sink,
no product test hook. For a remote stack `E2E_STACK_CODE_COMMAND` supplies the codes.

## Screenshots and manifest
The `flow` fixture saves `step(name)` screenshots and attaches them to the test. A custom
reporter (`e2e-stack/flow-reporter.ts`) writes `flows.json` in the same folder
(`apps/client/test-results/stack-flows` by default, `E2E_STACK_SHOTS_DIR` to change it).

## Gates are not papered over
Specs read `GET /v1/features`; a flow whose feature is off in the stack under test is marked
`test.fixme` with the reason, so the same suite reports the gate locally and on staging.

## Data, analytics, AI reliability
No product data: the e2e stack is disposable and its volumes are deleted on `down`; no backup.
Analytics are off (`ANALYTICS_OPT_OUT=true`), so no PostHog events are emitted. AI: the
default mock provider makes runs deterministic; live runs exercise the production OpenRouter
route and fallbacks and are kept small; no new AI behaviour, so no golden-set change.
