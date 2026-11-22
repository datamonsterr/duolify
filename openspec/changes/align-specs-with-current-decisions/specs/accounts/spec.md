## MODIFIED Requirements

### Requirement: Production-safe authentication boundaries
The API SHALL enforce production authentication and CORS behavior unless DUOLIFY_ENV is exactly development. Production SHALL require a strong configured signing secret and provisioned email delivery, and exclude browser localhost origins. Routes SHALL require authentication unless explicitly public. Google and Apple SSO SHALL use Clerk; the API MUST verify the Clerk session token signature, issuer, authorized party and expiry, and link only an eligible verified primary email or known Clerk subject as specified in ADR 0016. Email codes remain first-party. JWT verification SHALL reject unsigned tokens and algorithms other than HS256. JWT, OTP, OAuth-state, rate-limit and credential-encryption keys SHALL use separate HKDF purpose labels.

#### Scenario: Missing environment does not enable development
- **WHEN** the environment is unset or has a value other than development
- **THEN** placeholder secrets and browser localhost CORS are rejected

#### Scenario: Invalid identity claims
- **WHEN** Clerk token signature, issuer, authorized party or expiry is invalid, or an access JWT uses none or HS512
- **THEN** authentication is rejected
