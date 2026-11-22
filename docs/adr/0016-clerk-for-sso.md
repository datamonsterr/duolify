# ADR 0016: Clerk for SSO

- Status: Accepted (owner, 2026-11-22). Supersedes the OAuth part of ADR 0004.
- Date: 2026-11-22

## Context

ADR 0004 chose first-party auth and planned Google and Apple OAuth by hand (token exchange, ID-token validation, Apple client-secret JWTs, key rotation). That code never went live: both providers sat behind "unavailable" adapters waiting on provisioning. Each future social provider would repeat the work.

## Decision

- All social SSO (Google, Apple, later others) goes through Clerk. The first-party Google and Apple OAuth code, endpoints (`/v1/auth/google|apple/*`) and `APPLE_*` variables are removed.
- Email one-time code sign-in via Resend stays first-party. It is cheap, works, and needs no vendor session. Moving it to Clerk would add per-user cost and make the fallback depend on the vendor it is the fallback for.
- Duolify keeps its own accounts and sessions. Clerk only proves identity: the API verifies the Clerk session token itself (signature, issuer, authorized party, expiry; optional networkless `CLERK_JWT_KEY`), reads the user through the Clerk Backend API, then links or creates the Duolify learner and issues the existing Duolify session (rotation, revocation and session listing unchanged).
- Linking never trusts an unverified email. A known Clerk user id always wins (an email change at Clerk does not move the account). Otherwise Clerk's primary email address (`primary_email_address_id`, never a secondary address) must have `verification.status == verified` with strategy `email_code`, `from_oauth_google` or `from_oauth_apple`; admin, invitation and any other strategy are treated as unverified. If it matches an existing learner, that learner is linked once; a learner that already has a different Clerk user is never re-linked (409). This deliberately relaxes the earlier "never link on email" rule: a Clerk-verified email is as strong proof as our own email code.
- With no Clerk keys configured the feature is off: `GET /v1/auth/sso/config` reports `enabled:false`, SSO routes answer 503, and the client hides the buttons. Email codes are unaffected.

## Native flow (RFC 8252 + PKCE)

Clerk has no Tauri SDK, and provider consent screens refuse embedded webviews. The app therefore opens a Duolify web page, `/sso-bridge`, in the system browser. Clerk (`@clerk/clerk-js`) runs only there, on a normal https origin.

1. The app creates a PKCE verifier and calls `/sso/start` with the challenge; it gets a one-time `state`.
2. The bridge page signs the learner in with Clerk and posts the Clerk session token and `state` to `/sso/handoff`. The API verifies the token, links or creates the learner, and mints a one-time authorization **code** (random, stored only as a keyed hash, valid 2 minutes). The code is returned only in that response, to the browser that signed in.
3. The page returns to the app on that device through `duolify://sso/callback?state=...&code=...`. On the web build the same tab redeems it.
4. The app calls `/sso/complete` with `state`, `code` and the PKCE verifier. The code is compared in constant time, a wrong code or verifier counts as a try, the fifth failure burns the attempt, and success consumes it. There is no way to complete an attempt without the code, so polling does not work.

Cross-device phishing fails: an attacker who sends a victim the bridge link for the attacker's `state` never receives the code (it goes to the victim's browser and opens the victim's app, which ignores an unknown `state`). An app that intercepts the deep link on the same device has the code but lacks the verifier. The bridge page's "this signs you in on this device" text is UX only, not a control. Clerk's own session is signed out after hand-off.

## Consequences

- New vendor dependency and cost per monthly active user above the free tier; the first-party fallback limits the blast radius of a Clerk outage to social sign-in.
- Clerk user id and verified email are personal data: stored in `auth_provider_identities`, exported, erased with the learner, anonymized in nonproduction backups. Clerk keeps its own copy; erasing a Duolify account also deletes the Clerk user, through a durable queue drained by a worker (`openspec/changes/delete-clerk-user-on-erasure`). Erasure never waits on Clerk.
- Clerk dashboard settings the owner must configure are listed in `openspec/changes/clerk-sso-sign-in/design.md`.
