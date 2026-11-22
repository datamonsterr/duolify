# Security headers and Content-Security-Policy

## Why
Staging sends no security headers: no HSTS, CSP, `X-Content-Type-Options`, `Referrer-Policy` or framing protection on the web client or the API. Any injected script would run with the learner's session, and any site could frame the app.

## What changes
- The edge (Caddy) sends HSTS, `nosniff`, `Referrer-Policy`, `Permissions-Policy` and a real CSP on the web client. Origins (API, lesson asset origin, PostHog, Clerk) come from the existing environment variables, with no host names in the policy. Clerk's origins are allowed on `/sso-bridge` only.
- The API sends HSTS, `nosniff`, `Referrer-Policy: no-referrer`, `default-src 'none'; frame-ancestors 'none'` and `Cross-Origin-Resource-Policy` from one middleware.
- Lessons: a `srcdoc` iframe inherits the page's CSP, which would force `unsafe-inline` on the whole app. The player loads a sandbox host page (`/lesson-frame.html`) with its own header policy instead, and posts the slide to it. The iframe stays `sandbox="allow-scripts"` without `allow-same-origin`.
- `selfhost.sh smoke` asserts the headers after every deploy; the owner gets documented headers for the Cloudflare asset origin; ADR 0022 records the policy and each origin.
- Tests: Go middleware tests, a Caddy-based harness for the headers, and Playwright run under the production policy (lesson plays, sign-in loads, zero violations, an injected script is blocked).

## Impact
- Affected areas: `services/api/internal/httpapi`, `infra/deploy` (Caddyfile, `security-headers.caddy`, entrypoint, smoke), `apps/client` (`SandboxFrame`, `lesson-frame.html`, service worker, Vite preview, Playwright suites, Tauri CSP), `docs/infra`, `docs/adr`.
- Specs: `infra-hosting`, `lesson-player`.
- Data backup: no new data; the headers are configuration in the repository.
- Analytics: no new events. PostHog stays allowed for ingestion only.
- AI reliability: no AI behaviour changes; no new evals.
- Out of scope: HSTS preload, COOP/COEP, a CSP reporting endpoint, deploying to staging, Cloudflare and Clerk dashboards (documented for the owner).
