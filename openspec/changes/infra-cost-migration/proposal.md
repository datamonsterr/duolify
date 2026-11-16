# Infrastructure cost migration (E18)

## Why

Railway bills used CPU, RAM and $0.05/GB service egress. Slides and audio are streamed through the API, so delivery is billed as compute egress. Estimated prod cost is ~$70/month at 1k DAU and ~$310–450 at 10k DAU. The same stack on Hetzner Cloud VMs behind Cloudflare, with versioned Hetzner Object Storage, is estimated at ~$35 and ~$130–170. It also has lower database latency. Evidence and the full comparison: `docs/infra/cost-migration-options.md`. Decision draft: ADR 0014 (Proposed).

## What Changes

- **Phase 0, measure:** baseline Railway usage per service, a k6 load profile and p95/SSE timings.
- **Phase 1, edge:** Cloudflare DNS/CDN/WAF in front of the API, the web client on Workers static assets, cache headers for immutable lesson objects, SSE heartbeats under the Cloudflare 100 s proxy read timeout, and App Sleeping for non-prod Railway environments.
- **Phase 2, objects:** an S3 conformance suite (versioning, delete-marker restore, `If-None-Match: *`, Object Lock), then bucket migration to Hetzner Object Storage. No application code change.
- **Phase 3, staging on VMs:** a production compose overlay, a VM deploy job behind `ci-ok`, Postgres WAL archiving (PITR) to object storage and a restore drill on the VM.
- **Phase 4, prod cutover:** maintenance-mode write freeze (≤ 15 min), a final dump and restore, a Cloudflare origin flip, and Railway kept deployable for 30 days.
- **Phase 5, decommission:** remove Railway services and archive the `.railway/` IaC.
- No proto change. No learner-facing behaviour change.

## Impact

- **client:** the web build is deployed to Cloudflare Workers static assets. The Tauri apps are unchanged apart from the API base URL, which keeps its domain.
- **api:** cache headers on immutable object routes, an SSE heartbeat interval, and config for the new S3 endpoints. River, advisory locks and `pgx` are unchanged, and the API keeps direct Postgres connections (no transaction pooler).
- **engine:** redeployed as the same image on the VM private network. No code change. Evals must pass on the new host.
- **proto:** none.
- **infra:** `infra/deploy` compose overlay, WAL archiving config, runbooks, a new deploy workflow target, and mise tasks `infra:s3-conformance` and `infra:deploy-plan`.
- **ADRs:** 0014 (new, Proposed). Changes 0003 (storage provider, PITR) and 0008 (deploy target) once accepted. Keeps 0002.
- **Owner gates:** choice of option, account creation, credentials, DNS and the cutover window are `ready-for-human` tickets.
