## Context

Eight ADRs fix the stack: monorepo (0001), Go API plus Rust engine over gRPC (0002), Postgres, Redis, object storage and River (0003), first-party auth (0004), Tauri client with sandboxed Motion HTML (0005), Claude plus OpenRouter and Hugging Face golden set (0006), credits ledger (0007), two-tier CI (0008). Most are "Proposed (confirm)".

## Goals / Non-Goals

**Goals:**
- Each area builds, lints and tests through mise with a stub feature.
- The gRPC contract compiles on both sides.
- Draft CI under 5 minutes warm.

**Non-Goals:**
- Real auth, payments, production deploy, live eval content.

## Decisions

- One work package per area with a disjoint directory, so packages run in parallel.
- Stubs prove the vertical slice: API health calls Engine health; client player loads a sample Slide.
- Mock provider first, so evals and e2e run without API cost.

## Risks / Trade-offs

- ADRs not yet confirmed may change; scaffolds stay thin to limit rework.
- Two Cargo workspaces double Rust cache cost (ADR 0001).
