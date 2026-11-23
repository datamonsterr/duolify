# Configure Railway environments and release gates (E11-06)

## Why
Extend the bootstrap Railway infrastructure with reviewable environment
separation across dev, staging, and prod while keeping live deployment and
account secrets owner-controlled.

## What Changes
- Provide git-backed dev, staging, and prod Railway environment definitions
  with variables by name only, roles for Postgres, Redis, and S3, and private
  networking ensuring the AI Engine is never publicly exposed.
- Add a plan-only validator (Go in services/api/internal/backup and Rust check
  in engine/crates/engine-core/src/backup) that rejects a public engine, missing
  dependencies, or plaintext secrets in config, and redacts secrets in output.
- Expose environment plan validation through a mise task and wire into CI as
  plan-only evidence preserving `ci-ok` and the draft/ready tiers.
- Enforce release gate rules: any live apply step is gated on owner HITL ticket
  E11-07 and stays disabled in AFK implementation.

## Impact
Railway IaC configuration in `.railway/`, API backup package, Engine core
backup crate, mise infra tasks, CI workflow checks, and data inventory docs.
No new AI behavior or learner tables.
