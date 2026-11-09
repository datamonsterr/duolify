# Design

Postgres serializes requests by hashed email/IP rate-limit keys, challenge row locks
serialize verification, and identity uniqueness prevents duplicate Learners. Codes
expire in 10 minutes, allow five attempts, and are HMAC-SHA256 hashed with the server
secret. New requests replace old codes. Requests allow three per email and twenty
per IP per hour. Refresh secrets have 256 bits of entropy, SHA256 hashes, a 30-day
expiry and rotate atomically. Reuse revokes the session family. Access JWTs last
15 minutes and validate algorithm, issuer, audience, expiry and active session.

Age confirmation (16+) is enforced by both client and API. Profile defaults use
validated device locale and IANA time zone. Native desktop uses a narrow Tauri
bridge to the maintained Rust keyring crate; browsers use disposable session storage
with an in-memory fallback. Native
mobile fails closed pending a supported credential implementation (parent decision).

Auth/profile rows join encrypted nightly Postgres dumps and cascade on Learner
deletion; challenge and rate-limit rows expire and are pruned during requests.
Restore tests check expiry, hashed secrets and deletion. No new AI behavior,
provider calls, eval changes or fallback changes.

Analytics catalogue: `ui_screen_viewed(screen_id=welcome|sign_in)` and
`ui_control_activated(surface=sign_in,control_id=sign_in)`. Emit through existing
PostHog wrappers only with enabled internal UUID context, common platform,
app_version and source. No email, code, token, destination or text. A successful
sign-in supplies UUID then emits the sign-in screen/control events; anonymous
views remain silent. Preferences are honored before events and across refreshes.
