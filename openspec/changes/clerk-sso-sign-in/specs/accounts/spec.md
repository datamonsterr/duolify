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

### Requirement: Native callbacks belong to the pending attempt

The native handler SHALL ignore success and cancellation callbacks whose state does not match the current pending attempt. A cancellation deep link SHALL carry state. Startup deep links SHALL be read once per process, and listener setup SHALL NOT reactivate a completed or cancelled attempt.

#### Scenario: Retained callback from a previous attempt

- **WHEN** native deep-link startup replay supplies state S1 during pending attempt S2
- **THEN** S2 remains active and its later valid callback completes sign-in

### Requirement: Erasing Clerk subjects cannot be bound

Before linking or creating a learner, the API SHALL reject a Clerk subject with an accepted erasure intent, including pending SQL requests and durable external intents. Binding, erasure and deletion processing SHALL serialize the subject decision. A new provider sign-up after deletion has a new Clerk subject.

#### Scenario: Sign-in before the erasure worker runs

- **WHEN** the deleted learner's Clerk subject still exists at Clerk with deletion queued
- **THEN** handoff returns a generic unauthorized response and creates no learner or binding
