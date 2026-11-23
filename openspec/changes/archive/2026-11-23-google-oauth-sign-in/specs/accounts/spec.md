# Accounts Specification

## Purpose

Allow a Learner to sign in through Google in the system browser with provider verification, replay-safe transactions, and existing Duolify sessions.

## Requirements

## ADDED Requirements

### Requirement: Provider-neutral OAuth verification

The API SHALL expose provider-neutral authorization and verified-claim interfaces so Google sign-in can be implemented without coupling session issuance to a provider. The initial implementation SHALL use a deterministic Google fixture and SHALL NOT require live credentials; live provisioning is gated by E1-10.

#### Scenario: Provider fixture

- **WHEN** an OAuth transaction is completed with a valid fixture authorization code
- **THEN** the API returns verified provider claims through the provider interface and issues the existing Duolify session format

### Requirement: OAuth transaction integrity

Each OAuth attempt MUST bind unpredictable state, nonce, and PKCE verifier/challenge values, an exact allowlisted redirect, and a short expiry. The API MUST consume state once and reject replay, substitution, expiry, redirect mismatch, nonce mismatch, or invalid provider claims. Return targets MUST use the existing safe-path allowlist.

#### Scenario: Callback replay or substitution

- **WHEN** a callback reuses state, changes its redirect or code binding, or presents a different nonce
- **THEN** the API rejects it without issuing tokens or creating an identity binding

### Requirement: Verified provider identity binding

Provider identities MUST be uniquely bound by provider and provider subject. The API MUST require verified email claims. A verified email collision with another Learner MUST NOT silently merge accounts; it MUST return a generic linking-required response. Provider bindings MUST cascade with Learner deletion.

#### Scenario: Existing email collision

- **WHEN** a new provider subject presents a verified email already owned by a different Learner
- **THEN** the API rejects sign-in and leaves both Learner records and identities unchanged

### Requirement: System-browser sign-in and recovery

The client SHALL launch Google authorization through a system-browser bridge and SHALL accept completion only through the configured web or native callback bridge. Cancellation SHALL restore focus to the initiating control and leave credentials and account state unchanged. Loading, offline, retry, and error states SHALL remain keyboard and touch accessible at desktop and mobile widths.

#### Scenario: Learner cancels browser authorization

- **WHEN** the OAuth browser flow is cancelled
- **THEN** the client returns to the Welcome surface, restores focus, and permits email-code sign-in or retry

### Requirement: OAuth analytics privacy

The client SHALL emit `ui_control_activated` with `surface=sign_in` and `control_id=sign_in` when Google sign-in is started, and `ui_recovery_selected` with `surface=sign_in`, `error_code=auth_cancelled`, and `action=retry` when cancelled. Events MUST use existing PostHog wrappers and applicable UUID/common properties, respect opt-out, and MUST NOT contain email, authorization code, state, token, provider subject, or learner text.

#### Scenario: Analytics opt-out

- **WHEN** analytics is opted out during Google sign-in
- **THEN** neither start nor cancellation sends an event

### Requirement: OAuth persistence backup and deletion

Provider-subject bindings MUST be stored in Postgres, included in full database dumps, and cascade with Learner deletion. Temporary OAuth transaction records MUST expire promptly and MUST NOT retain provider access or refresh tokens. Backup restore checks MUST verify provider bindings without storing raw credentials.

#### Scenario: Restored provider binding

- **WHEN** a synthetic database dump is restored
- **THEN** the provider and subject binding remains associated with its Learner and no raw OAuth credential is present

### Requirement: AI behavior boundary

Google OAuth SHALL add no AI behavior and MUST preserve existing AI evaluation and fallback gates.

#### Scenario: Sign-in does not invoke AI

- **WHEN** a Learner signs in through Google
- **THEN** no Engine request or AI evaluation behavior is added
