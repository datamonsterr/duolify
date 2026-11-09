## ADDED Requirements

### Requirement: Active device sessions and revocation
Authenticated learners SHALL list active owned session families and revoke one or all devices. Responses MUST omit secrets. Access and refresh authorization MUST consult current revocation state, including after restore. Refresh consumption MUST be serialized; replay MUST revoke the family. Device credentials MUST be removed after successful sign-out.

#### Scenario: Concurrent refresh and replay
- **WHEN** concurrent requests use the same refresh secret
- **THEN** exactly one rotates and replay revokes that family, making its new tokens unusable

#### Scenario: Owned device revocation
- **WHEN** a learner revokes another active device or all devices
- **THEN** affected access and refresh tokens are rejected immediately and foreign session IDs cannot be revoked

### Requirement: Safe session recovery
Protected client requests SHALL share refresh work and retry once after access expiry. Recovery MUST preserve same-learner drafts and safe destinations, hide unauthorized content, discard drafts for a different learner, and recheck authorization on navigation and focus. Offline errors MUST offer Retry without clearing credentials. CORS MUST allow protected GET, POST, PATCH, PUT and DELETE methods for allowlisted origins.

#### Scenario: Session expires while editing
- **WHEN** refresh is rejected while an editor is mounted
- **THEN** sign-in recovery hides the editor and successful same-learner sign-in returns to the unchanged draft

### Requirement: Session backup and deletion
Nightly encrypted dumps SHALL include hashed families, consumed tokens, expiry and revocation state. Device secrets MUST NOT enter server backups. Restore checks MUST reject revoked and consumed credentials. Account deletion MUST cascade sessions and refresh hashes.

#### Scenario: Restored revoked family
- **WHEN** a consumed token or revoked family is restored
- **THEN** it remains unusable for authentication
