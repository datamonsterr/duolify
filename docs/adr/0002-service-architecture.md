# ADR 0002: Go API in front, Rust engine behind gRPC

- Status: Proposed (confirm)
- Date: 2026-11-08

## Context

Many learners need fast reads (courses, progress, streaks) while AI work (generation, chat) is slow and costly. The user chose Go for the router and multi-threaded services and Rust for the AI agent engine.

## Decision

- **API (`services/api`, Go)**: the only public entry point. REST/JSON over HTTP, Server-Sent Events for streamed chat and generation progress. Owns auth, courses, progress, streaks, credits ledger, notes, bookmarks, analytics events. Runs background workers (streak rollover, generation jobs, backups) in the same binary behind a `worker` subcommand, so they scale separately.
- **Engine (`engine`, Rust)**: private gRPC service (tonic). Stateless: receives full context in each request, calls LLM providers, returns content and token usage. No direct database access, so it scales horizontally and stays testable.
- **Contract**: `proto/duolify/engine/v1/engine.proto`, linted and checked for breaking changes with `buf`.
- **Credits**: API reserves credits before an engine call and settles with the actual usage the engine reports.
- **Speed**: Go handlers read hot data from Redis (streaks, leaderboards, session cache) and Postgres via pooled connections (pgx). Long work goes through the job queue, never the request path.

## Consequences

- Engine has no auth surface; network policy keeps it private (Railway private networking).
- All persistence logic is in Go; engine stays pure and easy to eval.
