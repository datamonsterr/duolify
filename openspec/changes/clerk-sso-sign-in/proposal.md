# Clerk SSO sign-in

Owner decision (2026-11-22, ADR 0016): all social SSO goes through Clerk. Replace the unprovisioned first-party Google and Apple OAuth with Clerk-verified sign-in. Email one-time codes via Resend stay first-party. Duolify keeps its own accounts and sessions; Clerk only proves identity.

Supersedes [google-oauth-sign-in](../google-oauth-sign-in/proposal.md) and [apple-oauth-sign-in](../apple-oauth-sign-in/proposal.md), and the OAuth part of ADR 0004.

## Impact

- **API:** Remove the Google and Apple provider code, fixtures, `/v1/auth/google|apple/*` and `APPLE_*` variables. Add `GET /v1/auth/sso/config` and `POST /v1/auth/sso/{start,handoff,complete}`; verify the Clerk session token server-side (official `clerk-sdk-go` v2), link or create the learner by Clerk user id and Clerk-verified email, then issue the existing Duolify session.
- **Client:** Provider buttons appear only when the API reports SSO enabled. A web bridge page runs Clerk in the system browser and returns by deep link `duolify://sso/callback`.
- **Data:** `auth_provider_identities.provider` becomes `clerk` (subject = Clerk user id). `auth_oauth_attempts` gains hand-off columns. Both are covered by export, erasure, backup anonymization and the restore drill.
- **Analytics:** Reuses the catalogued `ui_control_activated` and `ui_recovery_selected` sign-in events; no new events or properties.
- **AI / Engine / proto:** none. No evals or fallbacks apply.
- **Config:** `CLERK_SECRET_KEY`, `CLERK_PUBLISHABLE_KEY`, `CLERK_AUTHORIZED_PARTIES`, optional `CLERK_JWT_KEY`, optional `VITE_SSO_BRIDGE_URL`. No keys: feature off, email codes unaffected.
- **Migration:** `20261122130000_clerk_sso.sql` drops fixture Google/Apple bindings (none exist in production) and restricts providers to `clerk`.
