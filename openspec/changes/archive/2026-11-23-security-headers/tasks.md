## 1. Specs and records
- [x] 1.1 OpenSpec change and delta specs; main specs `infra-hosting` and `lesson-player` in sync
- [x] 1.2 ADR 0022 with the policy and the reason for each third-party origin
- [x] 1.3 Owner documentation: asset origin headers in `docs/infra/selfhost.md`

## 2. API
- [x] 2.1 Middleware tests: success, preflight, rejected origin, unauthenticated, CORS and cookie intact
- [x] 2.2 `setSecurityHeaders` in `httpapi.Middleware`

## 3. Edge
- [x] 3.1 `security-headers.caddy`, Caddyfile routes for the frame page and `/sso-bridge`
- [x] 3.2 `proxy-entrypoint.sh`, Dockerfile, compose and env examples
- [x] 3.3 `check-security-headers.sh`, `selfhost.sh smoke`, `test-security-headers.sh` (real Caddy)
- [x] 3.4 Tauri CSP and Workers `_headers`

## 4. Client
- [x] 4.1 `SandboxFrame` and `public/lesson-frame.html`; replace `srcDoc` in the player, course preview and shared demo
- [x] 4.2 Service worker primes the frame page; offline replay
- [x] 4.3 Vite preview sends the production headers; parser tests
- [x] 4.4 Playwright under the CSP (`e2e-session/csp.spec.ts`), stack flows fail on violations, F13 asserts headers
