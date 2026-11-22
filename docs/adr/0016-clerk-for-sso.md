# ADR 0016: Clerk for SSO

- Status: Accepted (owner, 2026-11-22). Supersedes the OAuth part of ADR 0004.
- Date: 2026-11-22

## Context

ADR 0004 chose first-party auth and planned Google and Apple OAuth by hand (token exchange, ID-token validation, Apple client-secret JWTs, key rotation). That code never went live: both providers sat behind "unavailable" adapters waiting on provisioning. Each future social provider would repeat the work.

## Decision

- All social SSO (Google, Apple, later others) goes through Clerk. The first-party Google and Apple OAuth code, endpoints (`/v1/auth/google|apple/*`) and `APPLE_*` variables are removed.
- Email one-time code sign-in via Resend stays first-party. It is cheap, works, and needs no vendor session. Moving it to Clerk would add per-user cost and make the fallback depend on the vendor it is the fallback for.
- Duolify keeps its own accounts and sessions. Clerk only proves identity: the API verifies the Clerk session token itself (signature, issuer, authorized party, expiry; optional networkless `CLERK_JWT_KEY`), reads the user through the Clerk Backend API, then links or creates the Duolify learner and issues the existing Duolify session (rotation, revocation and session listing unchanged).
- Linking never trusts an unverified email. A known Clerk user id always wins (an email change at Clerk does not move the account). Otherwise the Clerk primary email must be Clerk-verified. If it matches an existing learner, that learner is linked once; a learner that already has a different Clerk user is never re-linked (409). This deliberately relaxes the earlier "never link on email" rule: a Clerk-verified email is as strong proof as our own email code.
- With no Clerk keys configured the feature is off: `GET /v1/auth/sso/config` reports `enabled:false`, SSO routes answer 503, and the client hides the buttons. Email codes are unaffected.

## Native flow

Clerk has no Tauri SDK, and provider consent screens refuse embedded webviews. The app therefore opens a Duolify web page, `/sso-bridge`, in the system browser. Clerk (`@clerk/clerk-js`) runs only there, on a normal https origin. The page posts the Clerk session token to `/v1/auth/sso/handoff`, then returns to the app through `duolify://sso/callback?state=...`. The app holds a PKCE verifier and calls `/v1/auth/sso/complete`; the deep link carries no secret. On the web build the same page finishes the sign-in in the same tab. Clerk's own session is signed out after hand-off.

Residual risk: someone could send a victim a bridge link for the attacker's `state`. The page asks the learner to confirm they started sign-in in the app, and attempts expire after 10 minutes.

## Consequences

- New vendor dependency and cost per monthly active user above the free tier; the first-party fallback limits the blast radius of a Clerk outage to social sign-in.
- Clerk user id and verified email are personal data: stored in `auth_provider_identities`, exported, erased with the learner, anonymized in nonproduction backups. Clerk keeps its own copy; deleting a Duolify account should also delete the Clerk user (follow-up, not in this change).
- Clerk dashboard settings the owner must configure are listed in `openspec/changes/clerk-sso-sign-in/design.md`.
