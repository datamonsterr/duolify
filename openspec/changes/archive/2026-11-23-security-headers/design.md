# Design

## One source for the web headers
`infra/deploy/security-headers.caddy` holds three snippets (`web_headers`, `sso_bridge_headers`, `lesson_frame_headers`) with `{$VAR:default}` placeholders. The Caddyfile imports it; `apps/client/build/securityHeaders.ts` parses the same file so `vite preview` in Playwright sends the policy Caddy sends. The `Dockerfile` ships it with the Caddyfile. `proxy-entrypoint.sh` derives `CLERK_FAPI_ORIGIN` from `CLERK_PUBLISHABLE_KEY`.

## Why a separate lesson frame page
A `srcdoc` iframe inherits the parent's CSP (checked in Chromium: inline slide scripts are blocked under `script-src 'self'`). The slide needs inline code, the app must not. The sandbox host page is a real URL with its own header (`default-src 'none'`, inline allowed, network only to the asset origin, `frame-ancestors 'self'`). `SandboxFrame` hands it the slide by `postMessage` (`frame_ready`, `load_document`, `document_loaded`); the page `document.write`s it. The iframe attribute stays `sandbox="allow-scripts"`.

## Offline
Sandboxed frames have an opaque origin and never reach the service worker. The page is served `immutable` under `?v=<build id>`; the worker primes the HTTP cache. Playwright disables the HTTP cache when routes are active, so the mocked suites replay the first copy in `accounts-fixture.ts`; `e2e-session/csp.spec.ts` has no routes.

## API
`setSecurityHeaders` runs first in `httpapi.Middleware`, so every response (204 preflight, 403, 500, streams) has them; handlers can still `Set` a header. CORP is `same-site`: CORS `fetch` is not affected.

## Alternatives rejected
- `'unsafe-inline'` for scripts on the app: defeats the policy.
- Hashes or nonces for slides: slides are generated per learner; a static header cannot list them.
- `allow-same-origin` on the frame: forbidden by the lesson-player spec.
- A CSP in `public/_headers` for Workers: rules merge and cannot replace a header, so the frame and SSO pages cannot differ. Only the plain headers go there.
