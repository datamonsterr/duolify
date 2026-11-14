## ADDED Requirements

### Requirement: Railway environment separation and private engine isolation
The system SHALL provide git-backed definitions for dev, staging, and prod Railway
environments. Environment definitions MUST configure variables by NAME only, with
secret values preserved rather than committed. The AI Engine service MUST NOT define
public domains or routes and MUST be isolated to private internal networking.
Services SHALL specify Postgres, Redis, and separate S3 asset and backup roles.

#### Scenario: Validate environment separation
- **WHEN** the plan-only validator runs against dev, staging, and prod configurations
- **THEN** all environments declare required roles and the engine has no public domains

#### Scenario: Reject public engine
- **WHEN** an environment configuration exposes the engine on a public domain or route
- **THEN** the validator rejects the configuration

### Requirement: Release gates and plan-only CI evidence
CI SHALL enforce plan-only environment validation preserving `ci-ok` and draft/ready
tiers. Any live apply operation MUST be gated on owner HITL ticket E11-07 and remain
disabled. Config outputs and error reports MUST redact secret values. API readiness
`/readyz` and migration rollback SHALL guard service activation.

#### Scenario: Missing dependency or plaintext secret
- **WHEN** an environment lacks a required dependency or contains a plaintext secret value
- **THEN** the plan validator rejects the configuration and redacts secrets in error output

#### Scenario: Live apply disabled without E11-07
- **WHEN** a release gate evaluates an apply operation
- **THEN** the live apply step is rejected as disabled pending E11-07 approval
