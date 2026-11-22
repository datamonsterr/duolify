# Accounts Specification

## Purpose

Let a Learner sign in with Google or Apple through Clerk while Duolify keeps its own accounts and sessions, with email codes as the first-party fallback.

## Requirements

## ADDED Requirements

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

Each SSO attempt MUST bind an unpredictable one-time state, a PKCE challenge, the exact redirect `duolify://sso/callback` and a 10-minute expiry. Hand-off and completion MUST each succeed at most once per attempt. Completion MUST require the PKCE verifier. The deep link MUST carry only the state. Clerk tokens MUST NOT be persisted or logged.

#### Scenario: Replay or substitution

- **WHEN** a state is reused, expired, completed with a wrong verifier, or completed before hand-off
- **THEN** the API rejects it and issues no tokens

### Requirement: Account linking without takeover

A known Clerk user id MUST resolve to its Learner regardless of email. A new Clerk user MAY link to an existing Learner only when Clerk reports its primary email as verified and the email matches exactly one Learner with no other Clerk user bound. Otherwise the API MUST reject the hand-off and leave all records unchanged. An unknown verified email MUST create a Learner and binding in one transaction. The resulting session MUST be the existing Duolify session (rotation, revocation, session listing unchanged).

#### Scenario: Second Clerk user, same email

- **WHEN** a different Clerk user presents a verified email already linked to another Clerk user
- **THEN** the API returns 409 and issues no tokens

#### Scenario: Email-code account signs in with SSO

- **WHEN** a Learner who signed up with an email code signs in through Clerk with the same verified email
- **THEN** the same Learner is signed in and the Clerk user is bound to it

### Requirement: System-browser sign-in and recovery

The client SHALL run Clerk only on the web bridge page, opened in the system browser from the native apps, and SHALL accept completion only through the registered deep link or the same-tab web completion. Cancellation SHALL restore focus to the initiating control and leave credentials and account state unchanged. Loading, offline, retry and error states SHALL remain keyboard and touch accessible at desktop and mobile widths.

#### Scenario: Learner cancels browser sign-in

- **WHEN** the browser flow is cancelled
- **THEN** the client returns to the Welcome surface, restores focus and permits email-code sign-in or retry

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
