# accounts Specification

## Purpose
Let Learners sign in and keep their sessions safely. Sign-in uses email codes or Clerk-verified single sign-on, and Duolify issues and owns its own sessions, device revocation, backup and erasure.

## Requirements

### Requirement: Email-code sessions
The API SHALL authenticate Learners aged 16+ using single-use, expiring, hashed
email codes and rate-limit both requests and verification attempts. Verification
SHALL atomically create one identity, Learner and profile, issue a 15-minute JWT
and a hashed, rotating 30-day refresh secret. Anonymous protected reads MUST fail.
Refresh reuse and logout MUST revoke the session family.

#### Scenario: First verification
- **WHEN** a Learner verifies a valid code with age confirmation and device defaults
- **THEN** exactly one Learner/profile exists and authenticated reads return its UUID

#### Scenario: Invalid or expired code
- **WHEN** a code is expired, consumed, invalid or has exhausted its attempts
- **THEN** verification fails without issuing credentials

#### Scenario: Concurrent verification
- **WHEN** the same valid code is verified concurrently
- **THEN** exactly one verification succeeds

### Requirement: Welcome and secure credentials
The client SHALL present S01 welcome actions, email and code steps, age gate,
pending/error/offline states, retry/resend/change-email/cancel actions, keyboard
focus and equivalent desktop/mobile controls using theme tokens. Return paths
MUST be internal and allowlisted. Desktop credentials MUST use OS keyring; web
builds MAY use memory. Unsupported native storage MUST fail closed.

#### Scenario: Session recovery
- **WHEN** an access token expires and the refresh token is valid
- **THEN** credentials rotate and the intended protected route remains available

### Requirement: Private analytics
The client SHALL deliver catalogued screen and sign-in control events through
the shared PostHog adapter with internal UUID and common properties after sign-in.
Opt-out MUST suppress events and no email, code, token or Learner text may be emitted.

#### Scenario: Opted-out sign-in
- **WHEN** a Learner disables analytics before verifying
- **THEN** sign-in succeeds without emitting PostHog events

### Requirement: Auth backup and deletion
Identities, profiles, hashed challenges and sessions SHALL be included in encrypted
Postgres dumps with explicit expiry, 14-day backup retention and cascade deletion
of identity/profile/session data with the Learner. Restore checks MUST prove
expired codes stay unusable and no plaintext credential is stored. This feature
SHALL preserve existing AI evaluation and fallback gates without adding AI behavior.

#### Scenario: Restore and deletion
- **WHEN** auth data is dumped, restored and its Learner deleted
- **THEN** hashes and expiry are preserved and dependent personal records are removed

### Requirement: Active device sessions and revocation
Authenticated learners SHALL list active owned session families and revoke one or all devices. Responses MUST omit secrets. Access and refresh authorization MUST consult current revocation state, including after restore. Refresh consumption MUST be serialized; replay MUST revoke the family. Device credentials MUST be removed after successful sign-out.

#### Scenario: Concurrent refresh and replay
- **WHEN** concurrent requests use the same refresh secret
- **THEN** exactly one rotates and replay revokes that family, making its new tokens unusable

#### Scenario: Owned device revocation
- **WHEN** a learner revokes another active device or all devices
- **THEN** affected access and refresh tokens are rejected immediately and foreign session IDs cannot be revoked

### Requirement: Safe session recovery
Protected client requests SHALL share refresh work and retry once after access expiry. Recovery MUST preserve same-learner drafts and safe destinations, hide unauthorized content, discard drafts for a different learner, and recheck authorization on navigation and focus. Offline errors MUST offer Retry without clearing credentials. CORS MUST allow protected GET, POST, PATCH, PUT and DELETE methods for allowlisted origins.

#### Scenario: Session expires while editing
- **WHEN** refresh is rejected while an editor is mounted
- **THEN** sign-in recovery hides the editor and successful same-learner sign-in returns to the unchanged draft

### Requirement: Session backup and deletion
Nightly encrypted dumps SHALL include hashed families, consumed tokens, expiry and revocation state. Device secrets MUST NOT enter server backups. Restore checks MUST reject revoked and consumed credentials. Account deletion MUST cascade sessions and refresh hashes.

#### Scenario: Restored revoked family
- **WHEN** a consumed token or revoked family is restored
- **THEN** it remains unusable for authentication

### Requirement: System-browser sign-in and recovery

The client SHALL run Clerk only on the web bridge page, opened in the system browser from the native apps, and SHALL accept completion only through the registered deep link or the same-tab web completion. Cancellation SHALL restore focus to the initiating control and leave credentials and account state unchanged. Loading, offline, retry and error states SHALL remain keyboard and touch accessible at desktop and mobile widths.

#### Scenario: Learner cancels browser sign-in

- **WHEN** the browser flow is cancelled
- **THEN** the client returns to the Welcome surface, restores focus and permits email-code sign-in or retry

### Requirement: Production-safe authentication boundaries
The API SHALL enforce production authentication and CORS behavior unless DUOLIFY_ENV is exactly development. Production SHALL require a strong configured signing secret and provisioned email delivery, reject fixture OAuth providers in either provider slot, and exclude browser localhost origins. Routes SHALL require authentication unless explicitly public. Google identity validation SHALL check issuer, configured audience, expiry and the expected transaction nonce. JWT verification SHALL reject unsigned tokens and algorithms other than HS256. JWT, OTP, OAuth-state, rate-limit and credential-encryption keys SHALL use separate HKDF purpose labels.

#### Scenario: Missing environment does not enable development
- **WHEN** the environment is unset or has a value other than development
- **THEN** placeholder secrets, fixture providers and browser localhost CORS are rejected

#### Scenario: Invalid identity claims
- **WHEN** provider issuer, audience, expiry or nonce is wrong, or an access JWT uses none or HS512
- **THEN** authentication is rejected

### Requirement: Proxy-aware independent email throttles
The API SHALL ignore forwarded IP headers by default. An explicitly configured trusted-hop count SHALL select the client from the right of X-Forwarded-For, falling back to the socket address for short or invalid chains. Deployment SHALL prevent direct access around configured trusted proxies. Caller request limits SHALL be per IP and per email/IP pair; separate email delivery limits SHALL silently suppress excess delivery. Unexpired unconsumed codes SHALL be reused without resetting their expiry or attempt count. Resends SHALL have a 60-second cooldown.

#### Scenario: Spoofed forwarding prefix
- **WHEN** an attacker prepends addresses before the configured trusted proxy chain
- **THEN** those addresses do not change the selected client identity

#### Scenario: An attacker requests the victim's code
- **WHEN** a caller exhausts its own email/IP budget or requests another code during cooldown
- **THEN** the original code remains verifiable and another client's request budget is preserved

### Requirement: Bounded refresh race recovery
Refresh consumption SHALL be serialized. Repeated use of a consumed token within five seconds SHALL return the identical successor token pair while its session is active. Reuse outside that grace period SHALL revoke the session family. Successor token pairs SHALL be AES-GCM sealed with a separate derived key and token-bound associated data; plaintext SHALL never be persisted or logged.

#### Scenario: Concurrent refresh requests
- **WHEN** several requests concurrently refresh the same token within the grace period
- **THEN** all receive the same successor and the session remains active

#### Scenario: Late token replay
- **WHEN** a consumed token is reused outside the grace period
- **THEN** the request fails and the successor session is revoked

### Requirement: Credential backup and erasure
Encrypted backups SHALL cover authentication ciphertext, resend times, consumption state and email-linked rate hashes. Successor ciphertext SHALL be usable only during the grace period and cleared by auth maintenance after grace. Account deletion SHALL erase pending codes and email-linked throttle rows while preserving shared IP budgets. Production OAuth browser hosts SHALL exclude fixture test hosts. The Tauri script policy SHALL exclude unsafe-inline.

#### Scenario: Account authentication cleanup
- **WHEN** a learner deletes their account
- **THEN** pending codes and linked email rate-limit hashes are removed while unrelated learners and shared IP limits remain

#### Scenario: Browser reload
- **WHEN** the browser credential store is recreated
- **THEN** no credentials are restored from sessionStorage or localStorage

### Requirement: Clerk SSO is optional and fails closed

The API SHALL enable SSO only when Clerk is fully configured. Without Clerk keys the config endpoint SHALL report SSO as disabled, SSO routes SHALL return 503, the client SHALL hide SSO controls, and email-code sign-in SHALL be unaffected. A partial configuration SHALL stop API startup.

#### Scenario: No Clerk keys

- **WHEN** the API runs without Clerk keys
- **THEN** `GET /v1/auth/sso/config` returns `enabled: false`, SSO start returns 503 and the Welcome surface shows no Google or Apple button

### Requirement: Server-side Clerk verification

The API MUST verify every Clerk session token itself (signature, issuer, authorized party, expiry) and load the Clerk user before trusting an identity. The client MUST NOT be trusted for email, user id or verification status. Banned or locked Clerk users MUST be rejected.

#### Scenario: Forged or foreign token

- **WHEN** a hand-off carries a token with a wrong signature, issuer, authorized party or expiry
- **THEN** the API returns a generic 401 and creates no account, binding or session

### Requirement: SSO transaction integrity

Each SSO attempt MUST bind an unpredictable one-time state, a PKCE challenge, the exact redirect `duolify://sso/callback` and a 10-minute expiry. Hand-off MUST mint a single-use authorization code, stored only as a keyed hash, expiring within two minutes and returned only to the browser that completed Clerk sign-in. Completion MUST require state, code and PKCE verifier, MUST compare the code in constant time, MUST count failed tries and burn the attempt after five, and MUST consume the attempt on success. An attempt MUST NOT be completable without the code. Clerk tokens MUST NOT be persisted or logged.

#### Scenario: Replay or substitution

- **WHEN** a state or code is reused or expired, or completion uses a missing or wrong code, a wrong verifier, or happens before hand-off
- **THEN** the API rejects it and issues no tokens

#### Scenario: Cross-device phishing

- **WHEN** an attacker starts SSO in their own app and a victim signs in through the attacker's bridge link
- **THEN** the code is delivered only to the victim's browser, and the attacker's app, holding the state and verifier but not the code, cannot obtain a session

### Requirement: Account linking without takeover

A known Clerk user id MUST resolve to its Learner regardless of email. A new Clerk user MAY link to an existing Learner only when Clerk reports its primary email address (not a secondary address) as verified by `email_code`, Google or Apple, and the email matches exactly one Learner with no other Clerk user bound. Otherwise the API MUST reject the hand-off and leave all records unchanged. An unknown verified email MUST create a Learner and binding in one transaction. The resulting session MUST be the existing Duolify session (rotation, revocation, session listing unchanged).

#### Scenario: Second Clerk user, same email

- **WHEN** a different Clerk user presents a verified email already linked to another Clerk user
- **THEN** the API returns 409 and issues no tokens

#### Scenario: Email-code account signs in with SSO

- **WHEN** a Learner who signed up with an email code signs in through Clerk with the same verified email
- **THEN** the same Learner is signed in and the Clerk user is bound to it

### Requirement: SSO identity backup and deletion

Clerk bindings and hand-off attempts SHALL be learner-linked, exported without the Clerk user id, erased with the Learner, anonymized in nonproduction backups, and covered by the restore drill. Clerk tokens MUST NOT appear in any table, backup or log.

#### Scenario: Restored binding

- **WHEN** a synthetic database dump is restored
- **THEN** the Clerk binding remains tied to its Learner, nonproduction copies contain no real Clerk user id, and deleting the Learner removes the binding and attempts

### Requirement: SSO analytics privacy

Starting SSO SHALL emit the catalogued `ui_control_activated` (`surface=sign_in`, `control_id=sign_in`) and cancellation SHALL emit `ui_recovery_selected` (`surface=sign_in`, `error_code=auth_cancelled`, `action=retry`) through the PostHog wrappers, respecting opt-out. Events MUST NOT include email, state, token or Clerk identifiers.

#### Scenario: Opted out

- **WHEN** analytics is opted out during SSO
- **THEN** neither start nor cancellation sends an event

### Requirement: SSO AI boundary

Clerk sign-in SHALL add no AI behavior and MUST preserve existing evaluation and fallback gates.

#### Scenario: Sign-in does not invoke AI

- **WHEN** a Learner signs in through Clerk
- **THEN** no Engine request or evaluation behavior is added

### Requirement: Account erasure deletes the linked Clerk user

When a Learner's account is erased, or an erasure tombstone is re-applied after a restore, the API SHALL queue deletion of every Clerk user bound to the Learner in the same transaction that deletes the Learner. A worker SHALL delete queued Clerk users through the Clerk Backend API and remove each request once Clerk accepted it or reported the user already deleted.

#### Scenario: Clerk is unreachable at erasure

- **WHEN** a Learner with a Clerk binding erases the account while Clerk is down
- **THEN** the Learner and the binding are erased, the request answers 200 without calling Clerk, and the Clerk deletion stays queued for retry

#### Scenario: Retry succeeds

- **WHEN** Clerk is reachable on a later worker run
- **THEN** the Clerk user is deleted once, the request leaves the queue, and a further run makes no call

### Requirement: Clerk deletion is idempotent and off without Clerk

A Clerk user already deleted (404) SHALL count as success. Any other failure SHALL keep the request queued and increment its attempt count. Without a Clerk secret key the worker SHALL NOT be scheduled and no Clerk call SHALL be made; requests SHALL wait until Clerk is configured. Logs and errors MUST NOT contain a Clerk user id.

#### Scenario: Clerk not configured

- **WHEN** the API runs without `CLERK_SECRET_KEY` and a learner with a stale Clerk binding is erased
- **THEN** no Clerk call is made and the request waits in the queue

#### Scenario: Already deleted at Clerk

- **WHEN** Clerk answers 404 for a queued user
- **THEN** the request is removed as done

### Requirement: Web refresh credential travels only in a restricted cookie

For a client that declares `X-Duolify-Client: web`, the API SHALL deliver the refresh credential only as a cookie named `duolify_refresh` with `HttpOnly`, `Secure`, `SameSite=Strict`, `Path=/v1/auth/`, no `Domain` attribute and a `Max-Age` equal to the remaining life of the session. The API MUST NOT return the refresh credential in a response body to such a client. The client MUST NOT store a refresh credential in `localStorage`, `sessionStorage` or any script-readable location, and MUST keep the access token in memory only.

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
