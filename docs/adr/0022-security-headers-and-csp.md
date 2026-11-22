# ADR 0022: Security headers and Content-Security-Policy

- Status: Accepted (security-headers change, 2026-11-23)
- Date: 2026-11-23
- Change: `openspec/changes/security-headers`
- Numbering: 0020 is reserved for daily check-in credits; 0021 is voice input.

## Context

Staging sent no security headers: no HSTS, CSP, `X-Content-Type-Options`, `Referrer-Policy` or framing protection on the
web client or the API. The client plays generated Motion HTML slides, which are untrusted and need inline script and
style, so the policy has to protect the app without breaking playback. The web session is a `__Host-` refresh cookie.

## Decision

The edge (Caddy, `infra/deploy/security-headers.caddy`, imported by the Caddyfile) sets the web headers. The API sets its own in
one middleware (`httpapi.Middleware`). Origins are filled from the proxy's environment, never written as host names:
`LESSON_ASSET_ORIGIN`, `POSTHOG_HOST`, optional `API_ORIGIN`, and `CLERK_FAPI_ORIGIN`, which `proxy-entrypoint.sh` derives
from `CLERK_PUBLISHABLE_KEY` (base64 of the Frontend API host).

### Web client (all paths except the two below)

| Header | Value |
|---|---|
| `Strict-Transport-Security` | `max-age=31536000; includeSubDomains` (no `preload`: it is a one-way commitment for the whole domain) |
| `X-Content-Type-Options` | `nosniff` |
| `Referrer-Policy` | `strict-origin-when-cross-origin` |
| `Permissions-Policy` | `accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(self), payment=(), usb=()` (microphone for voice input, ADR 0021) |
| `Content-Security-Policy` | `default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: ASSET; media-src 'self' blob: ASSET; font-src 'self'; connect-src 'self' API ASSET POSTHOG; frame-src 'self'; worker-src 'self'; manifest-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'` |

No `unsafe-eval`, no inline script: the Vite build has none (checked by the Playwright suite under the policy).

Why each origin is there:

- **`'self'` for `connect-src`**: the client calls the API on its own origin (`/v1`, Caddy proxies), so the refresh cookie stays first-party.
  `API_ORIGIN` is empty unless the client is built with a separate `VITE_API_URL`.
- **`LESSON_ASSET_ORIGIN`** (`connect-src`, `img-src`, `media-src`): lesson assets, narration audio and the offline cache are read from the asset origin (R2 behind Cloudflare).
- **PostHog (`POSTHOG_HOST`, `connect-src`)**: `posthog-js` (the `module.no-external` build, so it loads no remote script) posts events to the ingestion host and reads its remote config from the assets host (`POSTHOG_ASSETS_HOST`, default `https://us-assets.i.posthog.com`; set `https://eu-assets.i.posthog.com` with an EU `POSTHOG_HOST`). Cloudflare's own Web Analytics beacon (`static.cloudflareinsights.com`), injected at the edge, is blocked by this policy on purpose; switch it off in the zone's Web Analytics settings to silence the console. `autocapture` and session recording are off.
- **Clerk, `/sso-bridge` only**: that page is the only place Clerk runs (ADR 0016), so its CSP adds `CLERK_FAPI_ORIGIN`, `https://challenges.cloudflare.com` and `https://*.protect.clerk.com` (script, connect with `:*`, frame), `https://img.clerk.com` (images) and `worker-src blob:`, as in Clerk's CSP guide. Other pages have no Clerk origin. Sign-in with an email code needs none.
- **`style-src 'unsafe-inline'`**: the one loosening of the app policy. Clerk injects CSS-in-JS and the UI libraries set `<style>`; style injection is a much smaller risk than script injection. Revisit with nonces if the build can emit them.
- **`form-action 'self'`**: Clerk redirects by navigation, not form post, so no Clerk entry is needed.

### Lesson slides: `/lesson-frame.html`

A `srcdoc` iframe **inherits the embedding page's CSP**. Slides need inline script, so with a strict app policy every lesson would break
(`Executing inline script violates ... script-src 'self'`; verified in Chromium), and the only fix inside the app would be `'unsafe-inline'` for everything.
So the player loads a real page, `/lesson-frame.html?v=<build id>`, in `<iframe sandbox="allow-scripts">` (still no `allow-same-origin`).
The page posts `frame_ready`; the app answers with the slide document; the page writes it (`document.write`), so the slide runs under the page's own header policy.
(`SandboxFrame`, `public/lesson-frame.html`.)

| Header | Value |
|---|---|
| `Content-Security-Policy` | `default-src 'none'; script-src 'unsafe-inline' data: ASSET; style-src 'unsafe-inline' ASSET; img-src data: ASSET; media-src data: ASSET; font-src data: ASSET; connect-src ASSET; frame-src 'none'; object-src 'none'; worker-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'` |
| `Cache-Control` | `public, max-age=31536000, immutable` (the URL carries the build id) |
| others | HSTS, `nosniff`, `Referrer-Policy: no-referrer`, `Permissions-Policy` with everything off |

`data:` in `script-src` is for slides cached on the device, whose assets are inlined (`sandboxDocument`); inline script is already allowed here.
The slide's own `<meta>` CSP stays and narrows this further. The slide can reach nothing but the asset origin and cannot read app storage (opaque origin).

**Offline.** A sandboxed frame has an opaque origin and Chromium never routes its navigation through the service worker. The immutable, build-versioned URL lets the
HTTP cache replay it; the worker primes that cache on install and when a lesson is opened or downloaded. Eviction of the browser cache shows the player's existing "slide didn't load" recovery with Retry.

### API

`Strict-Transport-Security: max-age=31536000; includeSubDomains`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`,
`Content-Security-Policy: default-src 'none'; frame-ancestors 'none'`, `Cross-Origin-Resource-Policy: same-site`.
Set at the start of `httpapi.Middleware`, so preflight (204), a rejected origin (403), panics (500) and streams carry them. CORP only affects no-cors embeds;
CORS `fetch` from the web client and the native apps is unchanged, and cookies are untouched. Caddy adds nothing on `/v1`, so no header appears twice.

### Other places the client is served

- **Tauri** (`tauri.conf.json`): added `object-src 'none'; base-uri 'self'; form-action 'self'`. Tauri has one static CSP, so it cannot give the frame page its own policy.
  The native player was already subject to `script-src 'self'` through the old `srcdoc` inheritance; check on a device before relying on it (follow-up).
- **Workers static assets** (`wrangler.jsonc`, owner-run deploy): `public/_headers` sets the headers that need no per-environment value. It carries **no CSP**: `_headers` rules merge and cannot replace
  a header, so the frame and SSO pages cannot get their own policy there. To serve from Workers with a CSP, move the frame to its own host or add a Worker that sets headers. Not deployed today; staging uses Caddy.
- **Asset origin** (R2 behind Cloudflare): owner setup, see `docs/infra/selfhost.md` ("Security headers").

## Testing

- Go: `httpapi` tests assert the headers on success, preflight, rejected origin, unauthenticated routes, and that CORS and cookies are intact.
- `infra/deploy/test-security-headers.sh` runs the real Caddy image with the repository config and the checker `selfhost.sh smoke` runs after every deploy
  (`check-security-headers.sh`), for web, SSO, frame and API headers.
- `apps/client/e2e-session/csp.spec.ts` runs the production headers (same snippet, read by `vite preview`) in Chromium against the real API: sign-in page loads, a lesson plays with its slide script running, an injected inline script is blocked,
  and there are zero CSP violation messages. The full-stack flows (`e2e-stack`, through Caddy) fail on any violation and F13 asserts the headers.

## Consequences

- A new third-party origin needs a line in `security-headers.caddy` and an entry here. A blocked request shows up as a CSP violation in the Playwright suites.
- `LESSON_ASSET_ORIGIN` is already fixed for the life of stored lessons (`selfhost.md`), so the frame page's long cache is safe.
- Not done: HSTS preload, `Cross-Origin-Opener-Policy` / COEP (Clerk and OAuth flows), CSP reporting endpoint, nonces for styles.
