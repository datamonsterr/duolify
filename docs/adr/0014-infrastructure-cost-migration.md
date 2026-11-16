# ADR 0014: Infrastructure cost migration off Railway

- Status: Proposed (owner must choose)
- Date: 2026-11-16
- Evidence: [docs/infra/cost-migration-options.md](../infra/cost-migration-options.md) (prices read 2026-11-16)
- Relates to: [0002](0002-service-architecture.md), [0003](0003-data-stores.md), [0008](0008-ci-and-deploy.md)

## Context

Railway bills used CPU (≈ $20/vCPU-month), RAM (≈ $10/GB-month) and $0.05/GB service egress. Slides and audio are streamed through the API, so delivery is billed as service egress. Estimated prod cost: ~$20 at 100 DAU, ~$70 at 1k, ~$310–450 at 10k. The code also limits where we can move:

- Backups and asset restore need **S3 bucket versioning**. Cloudflare R2 does not support it.
- **Session-level** `pg_advisory_lock` and River need a direct or session-mode Postgres connection. Hyperdrive and transaction-mode poolers are out.
- The worker must be always-on.
- SSE streams need a proxy that tolerates long responses (Cloudflare's proxy times out after 100 s without data).

## Options

- **A. Cloudflare-heavy** (Workers + Containers + R2 + Neon + Upstash): ~$60 / ~$170 / ~$600; highest lock-in and most unknowns.
- **B. Fly.io + Supabase/Neon + Upstash** behind Cloudflare: ~$45 / ~$85–145 / ~$330–430.
- **C. Hetzner Cloud VMs** (existing compose stack) behind Cloudflare, Hetzner Object Storage (versioned): ~$20 / ~$35 / ~$130–170; most ops.
- **D. Railway trimmed** (Cloudflare CDN in front, static client on Workers, sleeping non-prod): ~$20 / ~$70 / ~$310–450.
- E. Cloud Run + managed DB: more expensive than B/C because the worker is always-on.

Costs are prod per month at 100 / 1k / 10k DAU.

## Proposed decision

Phased **D → C**:

1. Measure, then put Cloudflare and the static web client in front of Railway now.
2. Move objects to Hetzner Object Storage. No code change: versioning is kept.
3. Move compute and Postgres to Hetzner VMs when the Railway bill exceeds $60/month or sustained DAU reaches 500, after a green restore drill on staging.
4. Keep Railway deployable as a fallback for 30 days after the cutover.

R2 stays an option once the versioning code is replaced. Supabase Pro remains the managed-Postgres fallback if self-managed Postgres is rejected.

## Consequences

- Lower and more predictable cost at 1k–10k DAU, and lower DB latency (database on the LAN, no cold starts).
- We own OS patching, Postgres upgrades, WAL/PITR and failover. Runbooks and alerts become part of the change.
- `deploy-dev.yml` gains a VM target. `ci-ok` and GHCR publishing are unchanged. `.railway/` is archived after decommissioning.
- The owner must answer the open questions in the options doc (learner regions, self-managed DB, credits eligibility, freeze window) before Status moves to Accepted.
