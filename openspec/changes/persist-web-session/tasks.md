## 1. Tests first

- [x] 1.1 Playwright (`mise run client:e2e-session`, same-origin build with `/v1` proxied): sign in, cookie attributes, reload, typed URL, new tab, sign out, reload stays signed out.
- [x] 1.2 Real-stack flow F1c (`apps/client/e2e-stack/f1c-session-persistence.flow.ts`) for the same journey on the Caddy layout.
- [x] 1.3 API integration (Postgres): exact cookie attributes and body, refresh rotates via cookie, reuse revokes the family and clears the cookie, cross-site and foreign `Origin` rejected, `GET` refused, native flow unchanged and no downgrade either way, logout / sign out everywhere clear the cookie.
- [x] 1.4 Unit: origin and Fetch Metadata gate without a database, no configured origin, client restore, rejected cookie, offline first load, logout, shared refresh, cross-origin build keeps the body flow.

## 2. Implementation

- [x] 2.1 `services/api/internal/accounts/websession.go`; wire `verify`, `sso/complete`, `refresh`, `logout`, sessions revocation and account deletion; `API_ALLOWED_ORIGINS` into `ConfigureWebOrigins` (API and test server).
- [x] 2.2 Client: cookie mode, restore on load, no refresh credential in script, no offline identity for cookie sessions.
- [x] 2.3 `mise run client:e2e-session` and its run inside `client:e2e` (shard 1).
- [x] 2.4 ADR 0004 amendment, `docs/data-inventory.md`, `.env.example` comment.

## 3. Data and analytics

- [x] 3.1 No schema, backup, export or analytics change; existing session-family evidence applies.

## 4. Owner steps

- [ ] 4.1 Confirm `PUBLIC_ORIGIN` (compose) equals the public web origin on staging and production, so cookie mode accepts it.
