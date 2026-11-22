## ADDED Requirements

### Requirement: Security headers on the web client
The edge proxy SHALL send on every web client response `Strict-Transport-Security: max-age=31536000; includeSubDomains` (no `preload`), `X-Content-Type-Options: nosniff`, `Referrer-Policy: strict-origin-when-cross-origin`, a `Permissions-Policy` that enables only `microphone=(self)` and turns camera, geolocation and the other sensors off, and a `Content-Security-Policy` with `default-src 'self'`, `script-src 'self'` (no `unsafe-inline`, no `unsafe-eval`), explicit `connect-src`, `img-src`, `media-src` and `frame-src`, `object-src 'none'`, `base-uri 'self'`, `form-action 'self'` and `frame-ancestors 'none'`. Origins other than `'self'` (lesson asset origin, PostHog, an optional API origin) MUST come from the deploy environment, not from host names in the policy. Clerk origins MUST be allowed on `/sso-bridge` only. `infra/deploy/security-headers.caddy` SHALL be the only definition of these values, shared with the browser test server.

#### Scenario: Web response
- **WHEN** a browser requests `/` or any client route from the proxy
- **THEN** the response carries each header above exactly once and the CSP has no `unsafe-eval` and no `unsafe-inline` in `script-src`

#### Scenario: Clerk only on the SSO page
- **WHEN** the CSP of `/sso-bridge` and of `/` are compared
- **THEN** only the former lists the Clerk Frontend API origin, `challenges.cloudflare.com` and `*.protect.clerk.com`

### Requirement: Security headers on the API
Every API response, including CORS preflight, rejected origins, errors and streams, SHALL carry `Strict-Transport-Security: max-age=31536000; includeSubDomains`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `Content-Security-Policy: default-src 'none'; frame-ancestors 'none'` and `Cross-Origin-Resource-Policy: same-site`, set once by the API middleware. The proxy MUST NOT add them again on `/v1`. CORS behaviour and the `__Host-` refresh cookie MUST be unchanged.

#### Scenario: Preflight and cookie flow
- **WHEN** the web client sends a preflight and then a credentialed refresh request from an allowed origin
- **THEN** both responses carry the security headers once, the CORS headers and the refresh cookie are as before

### Requirement: Deploy verifies the security headers
`selfhost.sh smoke` (run by every deploy) SHALL fail when the web client, `/lesson-frame.html`, `/sso-bridge` or the API lacks the required headers, sends one twice, or when the web CSP omits the asset origin. `infra:deploy-plan` tests SHALL run the real proxy image against the repository config and the same checker.

#### Scenario: Missing header after a deploy
- **WHEN** the proxy serves a web response without `Content-Security-Policy`
- **THEN** smoke reports the failing header and the deploy does not pass

### Requirement: Lesson asset origin headers are documented
The operator documentation SHALL list the response headers the lesson asset origin must send (HSTS, `nosniff`, `Referrer-Policy: no-referrer`, `Cross-Origin-Resource-Policy: cross-origin`, a restrictive `Content-Security-Policy`, CORS `*` for GET and HEAD) and where to set them in Cloudflare. The repository MUST NOT change Cloudflare.

#### Scenario: Owner sets up the asset host
- **WHEN** the owner follows `docs/infra/selfhost.md`
- **THEN** every header the player needs on the asset origin is listed with its value and reason
