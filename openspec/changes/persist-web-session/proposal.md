# Persist the web session across page loads

A signed-in Learner on the web build is signed out by any full page load: a reload, a typed URL, a new tab, a link that opens `<a href>`, or the SSO redirect returning to the tab. Credentials live only in a module variable (`apps/client/src/features/accounts/secure-store.ts`), so the web app is unusable for normal use. The Tauri build uses the native secure store and is fine. A real-stack test found it (`apps/client/e2e-stack/f1b-session-navigation.flow.ts`).

ADR 0004 and ADR 0011 (E1-163) chose in-memory browser credentials on purpose, to keep tokens out of script-readable storage. This change keeps that goal and removes the cost: the access token stays in memory only, and the refresh credential moves into an `HttpOnly` cookie that script cannot read. See the ADR 0004 amendment.

## Impact

- **API (`services/api/internal/accounts`):** A client that sends `X-Duolify-Client: web` is in cookie mode. `POST /v1/auth/verify`, `POST /v1/auth/sso/complete` and `POST /v1/auth/refresh` then set the `duolify_refresh` cookie and return only `access_token` and `expires_in`. `POST /v1/auth/refresh` and `POST /v1/auth/logout` read the credential from that cookie. Cookie mode requires a same-origin Fetch Metadata value and an exact configured `Origin`. Logout, "sign out everywhere", revoking the current session and account deletion clear the cookie. Native clients send no header and keep the body flow unchanged.
- **Client (`apps/client`):** In cookie mode `session.load()` asks the refresh endpoint for an access token when nothing is in memory, so a full page load restores the session. No refresh credential reaches script, `localStorage` or `sessionStorage`.
- **Config:** Cookie mode accepts the origins in `API_ALLOWED_ORIGINS` (already the CORS allow-list; `PUBLIC_ORIGIN` in the deploy compose). No new variable.
- **Data and backup:** No schema change. Refresh hashes, rotation, reuse detection and grace window are unchanged, so the existing backup, restore and erasure evidence still applies.
- **Analytics:** none. **AI / Engine / proto:** none; no evals or fallbacks apply.
- **Tests:** a Playwright suite (`mise run client:e2e-session`) and a real-stack flow (F1c) prove the session survives reload, typed URL and a new tab.

Out of scope: a cross-origin web deployment (the cookie needs the API on the page origin, as Caddy serves it), CORS credential support, and persisting the access token.
