# ADR 0001: Single monorepo, one directory per deployable

- Status: Proposed (confirm)
- Date: 2026-11-08

## Context

Duolify has four parts with different toolchains: a Tauri client (TypeScript + Rust shell), a Go API, a Rust AI engine and infra. Agents work on them in parallel.

## Decision

One repository. Top-level directories: `apps/client`, `services/api`, `engine`, `proto`, `evals`, `infra`, `openspec`, `docs`. No Turborepo or Nx: `mise` pins tools and runs tasks; each area owns `.mise/tasks/<area>.toml` and exposes `<area>:lint|test|build`. The engine is its own Cargo workspace; the Tauri shell has its own `Cargo.toml` under `apps/client/src-tauri` so client builds do not compile engine crates.

## Consequences

- One PR can change the gRPC contract and both sides together.
- CI uses path filters to run only affected areas.
- Two Cargo workspaces mean two Rust caches in CI; accepted to keep client builds small.
