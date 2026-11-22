# Duolify client

Run `mise run client:dev` for the Tauri app or `pnpm dev` in this directory for the web client. Development uses mock API data. The client reads `VITE_API_URL`, `VITE_POSTHOG_KEY`, and `VITE_POSTHOG_HOST` at build time.

## Full-stack flow tests

`e2e/` is the fast suite against a test API with a fixture engine. `e2e-stack/` drives the
real web client on a real stack (API, worker, Rust engine, Postgres, Redis, RustFS) with no
request mocking, and saves a screenshot per step:

```sh
mise run client:e2e-stack-up     # local stack on http://localhost:18471 (mock AI provider)
mise run client:e2e-stack        # screenshots + flows.json in test-results/stack-flows
mise run client:e2e-stack-down
```

Against a deployed environment, skip the local stack and set the URL and a way to read
sign-in codes: `E2E_STACK_URL=https://staging.duolify.online E2E_STACK_CODE_COMMAND='...'
mise run client:e2e-stack`. `E2E_LIVE=1` uses the live OpenRouter route. Write new flows as
`e2e-stack/<id>-<name>.flow.ts` with the `test`, `flow.step()` and `signUp()` helpers from
`e2e-stack/stack.ts`; see [docs/ci.md](../../docs/ci.md#full-stack-flow-suite).

On Debian/Ubuntu, Tauri requires `libwebkit2gtk-4.1-dev`, `build-essential`, `curl`, `wget`, `file`, `libxdo-dev`, `libssl-dev`, `libayatana-appindicator3-dev`, and `librsvg2-dev`. See [Tauri prerequisites](https://tauri.app/start/prerequisites/) for other platforms.

After installing the Android SDK, NDK and Rust targets described in the prerequisites, run `pnpm tauri android init` in this directory. On macOS, install Xcode, CocoaPods and the iOS Rust targets, then run `pnpm tauri ios init`. Generated mobile projects are local build artifacts and are not committed.
