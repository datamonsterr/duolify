## 1. Tests first

- [x] 1.1 Playwright: SSO cancel/recovery at 1280/390/320 light and dark, error retry, and callback completion with only `state` in the deep link (replaces E1-08/E1-09 OAuth specs).
- [x] 1.2 API integration (Postgres): feature off without keys, start validation, hand-off and completion, replay, missing/wrong/expired/replayed code, brute-force burn, cross-device phishing, wrong verifier, unverified email, link existing email account, Clerk id wins after email change, second Clerk user cannot take over, expiry.
- [x] 1.3 Unit: Clerk verifier (signature, issuer, `azp`, expiry, subject mismatch, banned user, email verification), environment fail-closed, PKCE and redirect allowlist, client deep-link parser and bridge URL pinning.

## 2. Implementation

- [x] 2.1 Remove first-party Google/Apple OAuth code, fixtures, routes, tests, `APPLE_*` variables.
- [x] 2.2 Add Clerk verifier, SSO routes, linking transaction, migration `20261122100000_clerk_sso.sql`.
- [x] 2.3 Client: SSO config gate, `/sso-bridge` page, deep-link bridge, PKCE.
- [x] 2.4 `.env.example`, `docs/data-inventory.md`, ADR 0016, ADR 0004 note.

## 3. Data and analytics

- [x] 3.1 Export, erasure, backup anonymization and restore-drill fixture cover `clerk` bindings and hand-off attempts.
- [x] 3.2 Reuse the catalogued sign-in analytics events; assert no state, token or subject in captured properties.

## 4. Owner steps

- [ ] 4.1 Configure the Clerk dashboard and set the four CLERK variables per environment (see design).
- [ ] 4.2 Live check: sign in with Google and Apple on dev, staging and a packaged desktop build (deep links work only in packaged apps on macOS and Linux).
- [x] 4.3 Follow-up: delete the Clerk user when a Duolify account is deleted (`openspec/changes/delete-clerk-user-on-erasure`).

## 5. Independent security review

- [x] 5.1 Reproduce retained native callbacks; ignore unrelated states including cancellation.
- [x] 5.2 Reject Clerk subjects awaiting erasure under the same subject lock.
