## ADDED Requirements

### Requirement: Turnstile for email code and sign-up
For `POST /v1/auth/code` and the email sign-up path at `POST /v1/auth/verify`, the client SHALL supply fresh Turnstile proof and the Go API MUST verify it server-side before sending or consuming a code, creating a Learner or issuing credentials. Verification MUST check the expected action and hostname and fail closed on missing, invalid, expired or unavailable proof. Existing rate limiting, age checks, identity protections and credential storage SHALL apply. The client MUST offer an accessible retry with a fresh token without exposing account existence. Tokens MUST be held only for verification and excluded from logs, analytics and backups.

#### Scenario: First email sign-up
- **WHEN** a Learner submits valid Turnstile proof for each action, a valid email code and age confirmation
- **THEN** the API verifies each action before its side effects and creates one Learner and session

#### Scenario: Failed proof
- **WHEN** the challenge proof is invalid or Siteverify is unavailable
- **THEN** the API sends or consumes no code, creates no account and returns a safe retry response
