# Design

## Flow

1. The app calls `POST /v1/auth/sso/start` with a PKCE challenge, the exact redirect `duolify://sso/callback`, age confirmation and first-sign-in profile defaults. The API stores a hashed one-time `state` (10 minutes) and returns it.
2. The app opens `https://<web origin>/sso-bridge?state=...&strategy=oauth_google|oauth_apple` in the system browser (Tauri opener, https only, origin pinned). On the web build it navigates the same tab (`mode=web`) and keeps the verifier in `sessionStorage`.
3. `/sso-bridge` loads `@clerk/clerk-js` with the publishable key from `GET /v1/auth/sso/config`, runs `signIn.authenticateWithRedirect`, handles the return with `handleRedirectCallback`, and gets `session.getToken()`.
4. The page posts `{state, session_token}` to `POST /v1/auth/sso/handoff`. The API verifies the token, loads the Clerk user, applies the linking rules, records the learner on the attempt and mints a one-time authorization code (random, stored as a keyed hash, valid 2 minutes) that is returned only in this response. The page signs out of Clerk.
5. Native: the page navigates to `duolify://sso/callback?state=...&code=...` on the device that signed in. The app checks the state, then `POST /v1/auth/sso/complete {state, code, code_verifier, redirect_uri}`. The API compares the code in constant time, checks PKCE, consumes the attempt and issues the Duolify tokens. Web: the page runs `complete` itself in the same tab.

## Why the system-browser bridge (Tauri)

Clerk documents Expo and Electron native SDKs, not Tauri. Its OAuth needs a normal https origin for cookies and redirects, and Google refuses embedded webviews. Running clerk-js inside the Tauri webview (`tauri://localhost`) would need that origin allowed in Clerk and still fail on provider consent screens. The system browser plus deep-link return is the pattern Clerk's own Electron and Expo SDKs use (OAuth deep links), and it reuses the deep-link plugin, opener and PKCE helpers already in the app. Clerk code stays out of the app bundle's critical path (dynamic import on one page).

Security model (RFC 8252 + PKCE): the verifier never leaves the app and the code never leaves the browser that signed in. Completion needs state, code and verifier. A wrong code or verifier counts as a try; five tries burn the attempt; success consumes it; the code expires after 2 minutes. An attempt cannot be completed by polling because there is nothing to redeem without the code.

## Verification

`clerk-sdk-go` v2: `jwt.Verify` with the instance JWK (from `CLERK_JWT_KEY` PEM, networkless, or JWKS fetched and cached for an hour), 5 s leeway, issuer check by the SDK, and an `azp` allowlist (`CLERK_AUTHORIZED_PARTIES`). Then `user.Get` for the primary email and its verification status (session tokens do not carry email). Banned and locked users are rejected. Verification failures map to a generic 401.

## Linking rules (no takeover)

1. A known Clerk user id resolves to its learner; the email is ignored.
2. Otherwise the Clerk **primary** email address (`primary_email_address_id`, never a secondary address) must have `verification.status == verified` with strategy `email_code`, `from_oauth_google` or `from_oauth_apple`. Admin-set, invitation and any other strategy are unverified; an unverified or missing email is rejected and nothing is written.
3. A matching Duolify email (the email-code account) is linked once. If that learner already has a different Clerk user bound, the hand-off is rejected with 409 and no tokens are issued.
4. No match creates a learner, profile and binding in one transaction.

Cross-device phishing (attacker sends a victim the bridge link for the attacker's `state`): the code is delivered to the victim's browser and device, so the attacker's app never sees it and cannot complete the attempt; the victim's app ignores an unknown `state`. This is covered by a named integration test.

## Failure and rollout

No Clerk keys: config reports disabled, routes return 503, buttons are hidden. Partial keys fail API startup. Rate limits reuse the verify budget per IP.

## Data, backup and privacy

Bindings and attempts are learner-linked (`ON DELETE CASCADE`), exported (binding without the subject), erased with the learner, anonymized (`subject`, `verified_email`) in nonproduction dumps; attempts are purged from nonproduction dumps. The restore drill fixture now carries a synthetic Clerk subject and a handed-off attempt and fails if the real values survive. No Clerk token is stored or logged.

## Owner configuration (Clerk dashboard)

- Social connections: enable Google and Apple (production needs your own Google OAuth client and Apple Services ID; development instances may use Clerk shared credentials).
- Email address: required, verified by the provider or email code; allow no unverified sign-in.
- Allowed origins / redirect URLs: `https://duolify.online/sso-bridge`, `https://staging.duolify.online/sso-bridge`, `http://localhost:1420/sso-bridge` (dev). Add the same origins to the instance `allowed_origins`.
- Native applications: the bridge is a web page, so the `duolify://` scheme is not given to Clerk; it is registered only in the Tauri deep-link plugin (`duolify`, already configured).
- Set `CLERK_AUTHORIZED_PARTIES` to the same web origins.
