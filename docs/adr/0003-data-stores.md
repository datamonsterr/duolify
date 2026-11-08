# ADR 0003: Postgres, Redis, S3-compatible object storage, Postgres job queue

- Status: Proposed (confirm)
- Date: 2026-11-08

## Decision

- **Postgres** (Railway Postgres; `postgres:18` locally): system of record. Access from Go with `pgx` + `sqlc`. Migrations with `goose` in `services/api/migrations`.
- **Redis** (Railway Redis; `redis:8` locally): cache, rate limits, leaderboards, streak counters. Never the only copy of data.
- **Object storage** (S3-compatible: Railway Buckets in cloud, RustFS locally): lesson slide HTML and assets, backups. MinIO was the first choice, but its public container images are no longer pullable (Docker Hub and Quay, 2026-11), so local dev uses RustFS; only the S3 API is used, so the store is swappable.
- **Job queue**: River (Postgres-backed Go queue). No extra broker; jobs commit atomically with app data.

## Backups

- Nightly `pg_dump` by the API `worker` to object storage, kept 14 days, plus Railway volume snapshots.
- Restore drill is a CI job on the ready-PR pipeline against a throwaway database (added with the first real migration).
- Every spec states what new data it creates and how it is backed up.

## Consequences

- Three stateful dependencies locally; docker compose runs all three.
