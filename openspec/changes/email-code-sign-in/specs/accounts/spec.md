## ADDED Requirements

### Requirement: Email-code sessions
The API SHALL authenticate Learners aged 16+ using single-use, expiring, hashed
email codes and rate-limit both requests and verification attempts. Verification
SHALL atomically create one identity, Learner and profile, issue a 15-minute JWT
and a hashed, rotating 30-day refresh secret. Anonymous protected reads MUST fail.
Refresh reuse and logout MUST revoke the session family.

#### Scenario: First verification
- **WHEN** a Learner verifies a valid code with age confirmation and device defaults
- **THEN** exactly one Learner/profile exists and authenticated reads return its UUID

#### Scenario: Invalid or expired code
- **WHEN** a code is expired, consumed, invalid or has exhausted its attempts
- **THEN** verification fails without issuing credentials

#### Scenario: Concurrent verification
- **WHEN** the same valid code is verified concurrently
- **THEN** exactly one verification succeeds

### Requirement: Welcome and secure credentials
The client SHALL present S01 welcome actions, email and code steps, age gate,
pending/error/offline states, retry/resend/change-email/cancel actions, keyboard
focus and equivalent desktop/mobile controls using theme tokens. Return paths
MUST be internal and allowlisted. Desktop credentials MUST use OS keyring; web
builds MAY use memory. Unsupported native storage MUST fail closed.

#### Scenario: Session recovery
- **WHEN** an access token expires and the refresh token is valid
- **THEN** credentials rotate and the intended protected route remains available

### Requirement: Private analytics
The client SHALL deliver catalogued screen and sign-in control events through
the shared PostHog adapter with internal UUID and common properties after sign-in.
Opt-out MUST suppress events and no email, code, token or Learner text may be emitted.

#### Scenario: Opted-out sign-in
- **WHEN** a Learner disables analytics before verifying
- **THEN** sign-in succeeds without emitting PostHog events

### Requirement: Auth backup and deletion
Identities, profiles, hashed challenges and sessions SHALL be included in encrypted
Postgres dumps with explicit expiry, 14-day backup retention and cascade deletion
of identity/profile/session data with the Learner. Restore checks MUST prove
expired codes stay unusable and no plaintext credential is stored. This feature
SHALL preserve existing AI evaluation and fallback gates without adding AI behavior.

#### Scenario: Restore and deletion
- **WHEN** auth data is dumped, restored and its Learner deleted
- **THEN** hashes and expiry are preserved and dependent personal records are removed
