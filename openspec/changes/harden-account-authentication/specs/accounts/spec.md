## ADDED Requirements

### Requirement: Production-safe authentication boundaries
The API SHALL enforce production authentication and CORS behavior unless DUOLIFY_ENV is exactly development. Production SHALL require a strong configured signing secret and provisioned email delivery, reject fixture OAuth providers in either provider slot, and exclude browser localhost origins. Routes SHALL require authentication unless explicitly public. Google identity validation SHALL check issuer, configured audience, expiry and the expected transaction nonce. JWT verification SHALL reject unsigned tokens and algorithms other than HS256. JWT, OTP, OAuth-state, rate-limit and credential-encryption keys SHALL use separate HKDF purpose labels.

#### Scenario: Missing environment does not enable development
- **WHEN** the environment is unset or has a value other than development
- **THEN** placeholder secrets, fixture providers and browser localhost CORS are rejected

#### Scenario: Invalid identity claims
- **WHEN** provider issuer, audience, expiry or nonce is wrong, or an access JWT uses none or HS512
- **THEN** authentication is rejected

### Requirement: Proxy-aware independent email throttles
The API SHALL ignore forwarded IP headers by default. An explicitly configured trusted-hop count SHALL select the client from the right of X-Forwarded-For, falling back to the socket address for short or invalid chains. Deployment SHALL prevent direct access around configured trusted proxies. Caller request limits SHALL be per IP and per email/IP pair; separate email delivery limits SHALL silently suppress excess delivery. Unexpired unconsumed codes SHALL be reused without resetting their expiry or attempt count. Resends SHALL have a 60-second cooldown.

#### Scenario: Spoofed forwarding prefix
- **WHEN** an attacker prepends addresses before the configured trusted proxy chain
- **THEN** those addresses do not change the selected client identity

#### Scenario: An attacker requests the victim's code
- **WHEN** a caller exhausts its own email/IP budget or requests another code during cooldown
- **THEN** the original code remains verifiable and another client's request budget is preserved

### Requirement: Bounded refresh race recovery
Refresh consumption SHALL be serialized. Repeated use of a consumed token within five seconds SHALL return the identical successor token pair while its session is active. Reuse outside that grace period SHALL revoke the session family. Successor token pairs SHALL be AES-GCM sealed with a separate derived key and token-bound associated data; plaintext SHALL never be persisted or logged.

#### Scenario: Concurrent refresh requests
- **WHEN** several requests concurrently refresh the same token within the grace period
- **THEN** all receive the same successor and the session remains active

#### Scenario: Late token replay
- **WHEN** a consumed token is reused outside the grace period
- **THEN** the request fails and the successor session is revoked

### Requirement: Credential backup and erasure
Encrypted backups SHALL cover authentication ciphertext, resend times, consumption state and email-linked rate hashes. Successor ciphertext SHALL be usable only during the grace period and cleared by auth maintenance after grace. Account deletion SHALL erase pending codes and email-linked throttle rows while preserving shared IP budgets. Browser credential fallback SHALL exist only in memory and SHALL ignore Web Storage credentials. Production OAuth browser hosts SHALL exclude fixture test hosts. The Tauri script policy SHALL exclude unsafe-inline.

#### Scenario: Account authentication cleanup
- **WHEN** a learner deletes their account
- **THEN** pending codes and linked email rate-limit hashes are removed while unrelated learners and shared IP limits remain

#### Scenario: Browser reload
- **WHEN** the browser credential store is recreated
- **THEN** no credentials are restored from sessionStorage or localStorage
