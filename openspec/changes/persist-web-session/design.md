# Design

## Decision

The access token stays in memory only. The web refresh credential is delivered and stored as a cookie. On start the web client calls the refresh endpoint; rotation and reuse detection from `session-rotation-revocation` apply unchanged (rotate on every use, revoke the family on reuse, 5-second grace for concurrent refreshes).

## Cookie

| Attribute | Value | Why |
|---|---|---|
| Name | `__Host-duolify_refresh` | Browser-enforced host binding prevents sibling subdomains from planting or shadowing a credential. |
| `HttpOnly` | yes | Script, including injected script, cannot read the credential. |
| `Secure` | yes | Never sent over plain HTTP. Browsers accept it on `localhost`, so dev and the e2e stack work. On a non-local plain-HTTP host the cookie is not stored and the web app keeps the in-memory session until reload. |
| `SameSite` | `Strict` | No documented flow needs `Lax`: the SSO bridge is a same-origin page, and the return to the app is a same-origin navigation followed by a same-origin fetch. |
| `Path` | `/` | Required by `__Host-`. Only gated auth endpoints read it; other endpoints ignore it. |
| `Domain` | none | Host-only. Web and API share one origin through Caddy (`/` web, `/v1` API), so no `Domain` is needed. The host prefix prevents parent-domain shadow cookies from sibling origins. |
| `Max-Age` | remaining life of the session family (30 days from sign-in; rotation does not extend it) | Matches the refresh lifetime stored in `auth_sessions.expires_at`. |

## Mode selection

The client declares its mode with the request header `X-Duolify-Client: web`. The existing code had no client-type header. Absence of the header means native.

- **Web mode** reads the refresh credential only from the cookie and ignores any body. It never returns a refresh credential in a response body.
- **Native mode** reads it only from the body, never sets the cookie, and ignores any cookie.

Neither mode can downgrade the other: a body token is useless in web mode, a cookie is useless in native mode, and the header alone grants nothing without a valid credential. A cross-site page cannot add the custom header without a CORS preflight, which the API refuses for its own origins' allow-list (`Access-Control-Allow-Headers` does not list it and no `Access-Control-Allow-Credentials` is sent).

The web client uses cookie mode only when the Tauri runtime is absent and the API base URL is the page origin (empty `VITE_API_URL`, the deployed layout). A cross-origin development build keeps the in-memory body flow, because credentialed cross-origin requests would need CORS credential relaxation, which is deliberately not added.

## Threat notes

- **CSRF.** Cookie-authenticated endpoints (`refresh`, `logout`) and the endpoints that set the cookie (`verify`, `sso/complete`) are `POST` only; a `GET` changes no state. In web mode each request must pass all of: header `X-Duolify-Client: web`; `Sec-Fetch-Site` absent or `same-origin` (so `same-site`, `cross-site` and `none` are refused); `Origin` present and exactly equal to a configured origin (no prefix or suffix match, no `null`). With no configured origin, cookie mode is refused. `SameSite=Strict` is the first layer; these checks are the second.
- **XSS.** An injected script can still use the session while the page is open (call the API with the in-memory access token or `fetch` refresh), as with any session. It cannot read or exfiltrate the refresh credential, so access ends when the page closes or the learner signs out, and the family can be revoked from the sessions screen.
- **Replay and theft of a cookie value.** Rotation on every use and reuse detection revoke the family when an old value reappears. The cookie is cleared on any 401 from refresh.
- **Login CSRF.** `verify` and `sso/complete` in web mode apply the same origin checks before consuming a code or an SSO attempt, so a cross-site request cannot burn a code or plant a session.
- **Logout failure.** The cookie is cleared only after the session was revoked in the database, so an outage cannot leave a live session whose cookie was already dropped.
- **Caches and logs.** Responses already carry `Cache-Control: no-store`. The cookie value is never logged; the access log records only request id and status.
- **Offline identity.** The offline-first identity cache binds to a hash of the refresh credential. A cookie session has none in script, so no offline identity is kept for it.

## Alternatives rejected

- `localStorage` or `sessionStorage` for the refresh token: script-readable; ADR 0011 forbids it.
- Persisting the access token in a cookie or storage: widens exposure for no gain, since refresh is cheap.
- A separate cookie per endpoint path: two credentials to rotate in step.
- `Domain` on the cookie, or `SameSite=None` with CORS credentials, to support a separate API host: not needed by the layout and a larger attack surface.

## Data, backup, analytics

No schema change, new data or events. Existing evidence covers it: refresh hashes and successor ciphertext are already in dumps and erased with the account; account deletion revokes the family and now also clears the cookie.

## Checked against

MDN `Set-Cookie` (attributes, `Max-Age`, `SameSite`), MDN Fetch Metadata (`Sec-Fetch-Site`), and Go `net/http` `Cookie` and `SetCookie` (negative `MaxAge` emits `Max-Age=0`).

## Security review amendment

Cookie prefix rules checked against [MDN Set-Cookie](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Set-Cookie) and [RFC 6265bis section 4.1.3.2](https://httpwg.org/http-extensions/draft-ietf-httpbis-rfc6265bis.html#name-the-__host-prefix). A host-only unprefixed cookie did not prevent sibling origins from planting a parent-domain cookie. The host prefix supplies that missing browser enforcement. Legacy credentials are ignored/expired and duplicate host cookies fail before auth work. Broad Path delivery confers no bearer authentication.

[Web Locks](https://developer.mozilla.org/en-US/docs/Web/API/Web_Locks_API) coordinate refresh, verify, SSO completion, logout and cookie-clearing revocations/erasure across tabs, through fetch and body processing. Unsupported browsers fail closed with an update message. The server's 5-second native/concurrent redemption grace is unchanged. Refresh checks `/v1/me` against the current learner before installing credentials or retrying requests; an account mismatch resets only the stale tab. BroadcastChannel was considered; the learner check is required even with broadcasts because a delayed 401 can outlive a notification. No cross-tab identity or credential is persisted in Web Storage.
