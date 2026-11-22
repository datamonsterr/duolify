# Accounts Specification

## Purpose

Keep a signed-in web Learner signed in across full page loads without putting a refresh credential where script can read it.

## Requirements

## ADDED Requirements

### Requirement: Web refresh credential travels only in a restricted cookie

For a client that declares `X-Duolify-Client: web`, the API SHALL deliver the refresh credential only as a cookie named `__Host-duolify_refresh` with `HttpOnly`, `Secure`, `SameSite=Strict`, `Path=/`, no `Domain` attribute and a `Max-Age` equal to the remaining life of the session. The API MUST NOT return the refresh credential in a response body to such a client. The client MUST NOT store a refresh credential in `localStorage`, `sessionStorage` or any script-readable location, and MUST keep the access token in memory only.

#### Scenario: Web sign-in

- **WHEN** a web client verifies an email code or completes SSO
- **THEN** the response sets the cookie with exactly those attributes and its body contains `access_token` and `expires_in` but no `refresh_token`

### Requirement: Web session restores after a full page load

When a web page starts with no access token in memory, the client SHALL call `POST /v1/auth/refresh` with no body. A valid cookie SHALL yield a new access token and a rotated cookie; an invalid or missing cookie SHALL yield 401, clear the cookie and show sign-in. Rotation, family revocation on reuse and the 5-second concurrent-refresh grace of `session-rotation-revocation` SHALL apply unchanged.

#### Scenario: Reload, typed URL, new tab and SSO return

- **WHEN** a signed-in Learner reloads, opens a typed URL, opens a new tab or returns from the SSO redirect in the same tab
- **THEN** the Learner is still signed in and is not sent to sign-in

#### Scenario: Reused cookie

- **WHEN** a previously rotated cookie value is replayed after the grace window
- **THEN** the API returns 401, clears the cookie and revokes the session family including its access tokens

### Requirement: Cookie-authenticated requests reject cross-site origins

In web mode the API SHALL refuse with 403, before reading or changing any state, a request whose `Sec-Fetch-Site` is present and not `same-origin`, or whose `Origin` is absent or not exactly equal to a configured origin, or when no origin is configured. The cookie-reading and cookie-setting endpoints SHALL accept only `POST`. The API MUST NOT send `Access-Control-Allow-Credentials`.

#### Scenario: Cross-site refresh

- **WHEN** a page on another site posts to the refresh or logout endpoint with the cookie attached
- **THEN** the API returns 403 and the session is unchanged

### Requirement: Native and web modes cannot downgrade each other

A request without `X-Duolify-Client: web` SHALL use the body refresh token exactly as before, never receive a `Set-Cookie`, and never have a cookie accepted in place of the body token. A web-mode request SHALL ignore any body refresh token.

#### Scenario: Native client

- **WHEN** the Tauri app signs in, refreshes and logs out
- **THEN** responses carry the refresh token in the body, no cookie is set, and behaviour is unchanged

### Requirement: Revocation clears the cookie

Logout, revoking the current session, "sign out everywhere" and account deletion SHALL clear the cookie for web clients. Logout SHALL clear it only after the session was revoked.

#### Scenario: Sign-out sticks

- **WHEN** a web Learner signs out and reloads
- **THEN** the cookie is gone, refresh returns 401 and the Learner stays on sign-in

### Requirement: Browser credential fallback

The access token of a browser session SHALL exist only in memory and SHALL ignore Web Storage credentials. This supersedes the `harden-account-authentication` rule that the browser holds all credentials in memory: the refresh credential is now held by the browser as an `HttpOnly` cookie, which script cannot read, so the intent (no script-readable credentials in storage) is kept.

#### Scenario: Stale storage

- **WHEN** `sessionStorage` or `localStorage` contains token-shaped values
- **THEN** no credentials are restored from them

### Requirement: Cookie ambiguity and legacy credentials fail closed

After the origin gate, cookie-relevant auth endpoints SHALL reject multiple host-prefixed refresh cookies with 400 before changing authentication state. The unreleased legacy `duolify_refresh` cookie SHALL be ignored and cleared at its original `/v1/auth/` path. Only refresh/logout read the refresh credential; verify/SSO completion may replace it. Other endpoints MUST NOT accept cookies as access authentication.

#### Scenario: Sibling plants a legacy credential

- **WHEN** a signed-out browser sends only a valid legacy refresh cookie planted by a sibling domain
- **THEN** refresh returns 401 and expires that cookie without accepting the attacker's learner

### Requirement: Cross-tab refresh preserves the learner and cookie order

The web client SHALL serialize all cookie-mutating authentication requests across tabs using an origin-wide Web Lock held through response processing. A browser without Web Locks SHALL fail cookie mutation with a recoverable error. Per-tab refresh SHALL remain single-flight. A refresh of an established in-memory learner SHALL verify the refreshed learner before storing its token or replaying any request; mismatch SHALL clear local credentials, drafts, caches and analytics identity and show sign-in without logging out the other tab's shared cookie. A plain HTTP host other than loopback SHALL retain the in-memory body flow.

#### Scenario: Another tab switches accounts

- **WHEN** learner A's pending mutation returns 401 after another tab signs in as learner B
- **THEN** A's tab resets and the mutation is never sent with B's bearer token

#### Scenario: A refresh response is delayed

- **WHEN** one tab has not processed its refresh response
- **THEN** another tab's refresh, sign-in or sign-out waits so an older response cannot overwrite a newer cookie
