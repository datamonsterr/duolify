# Apple Accounts Specification

## ADDED Requirements

### Requirement: Apple OAuth identity verification

The API SHALL use the existing provider-neutral OAuth interface and transaction for Apple sign-in. The Apple provider SHALL validate the identity token signature using Apple's current signing key set, retry key lookup after key rotation, and verify the exact Apple issuer, configured audience, expiry, and transaction nonce. Live Apple credentials SHALL remain disabled until E1-10; tests SHALL use deterministic provider and key-set fakes.

#### Scenario: Apple identity token is verified

- **WHEN** a callback contains an Apple token signed by a current or newly rotated fixture key with the expected issuer, audience, expiry, and nonce
- **THEN** the provider returns verified stable-subject claims and the API issues the existing Duolify session

#### Scenario: Apple token validation fails

- **WHEN** the Apple token has a wrong issuer, audience, nonce, expired lifetime, unknown key after refresh, or invalid signature
- **THEN** the callback is rejected without issuing tokens or binding an identity

### Requirement: Apple first-login and returning identity

The API SHALL identify an Apple account only by its verified stable subject. Apple email and name claims MAY be absent on a returning sign-in. A new Apple subject MUST include a verified, valid email before creating an account; the email may be Apple's private-relay address. A supplied first-login name SHALL be validated and stored as the Learner display name. Missing name SHALL use the neutral display-name default. Existing bindings SHALL sign in when Apple omits email or name and MUST NOT overwrite stored values with missing claims.

#### Scenario: Returning Apple identity omits first-login claims

- **WHEN** a previously bound Apple subject returns with no email or name
- **THEN** the existing Learner signs in and the stored email and display name remain unchanged

#### Scenario: New Apple identity shares a private-relay email

- **WHEN** a new Apple subject provides a verified private-relay email and optional name
- **THEN** the API creates one subject binding and account without special-casing or rejecting the relay domain

### Requirement: Apple account linking safety

The API MUST NOT infer linking from email alone. A new Apple subject whose verified email already belongs to another Learner MUST receive the generic linking-required response and MUST NOT create a binding or merge data.

#### Scenario: Apple email collision

- **WHEN** an unbound Apple subject presents an email owned by another Learner
- **THEN** sign-in requires explicit linking and both accounts remain unchanged

### Requirement: Apple callback and browser recovery

Apple sign-in SHALL reuse the existing exact callback URI, unpredictable state, nonce, PKCE, replay rejection, system-browser bridge, and secure OS credential storage. Cancellation SHALL restore focus to the Apple control without changing credentials. Loading, offline, retry, and error states SHALL remain keyboard and touch accessible at desktop and mobile widths.

#### Scenario: Learner cancels Apple authorization

- **WHEN** the browser authorization is cancelled
- **THEN** the learner returns to sign-in with focus restored and can retry or use email-code sign-in

### Requirement: Apple OAuth analytics privacy

The client SHALL emit the existing catalogued `ui_control_activated` sign-in event when Apple sign-in starts, and `ui_recovery_selected` with `surface=sign_in`, `error_code=auth_cancelled`, and `action=retry` when cancelled. Events MUST use the PostHog wrappers, UUID/common properties, and opt-out boundary, and MUST NOT include email, Apple subject, authorization code, state, nonce, identity token, name, or learner text.

#### Scenario: Apple analytics is opted out

- **WHEN** analytics is opted out during Apple sign-in
- **THEN** neither start nor cancellation sends a PostHog event

### Requirement: Apple identity backup and deletion

Apple subject bindings SHALL use the existing Postgres provider-identity table, be included in encrypted nightly dumps, and cascade with Learner deletion. Raw Apple tokens and client secrets MUST NOT be stored in OAuth records, backups, or analytics.

#### Scenario: Restored Apple binding

- **WHEN** a synthetic database dump is restored
- **THEN** the Apple subject remains bound to its Learner and deleting that Learner removes the binding

### Requirement: Apple sign-in AI boundary

Apple OAuth SHALL add no AI behavior and MUST preserve existing evaluation and fallback gates.

#### Scenario: Apple sign-in does not invoke AI

- **WHEN** a Learner signs in with Apple
- **THEN** no Engine request or AI evaluation behavior is added
