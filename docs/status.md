# Project status

Updated 2026-11-22. GitHub issues and pull requests are the source of truth for current work and evidence; the earlier claim that all remaining work waits on the owner is obsolete.

Staging is deployed on a self-hosted Hetzner `cx23` in Falkenstein behind Cloudflare, with Cloudflare R2 for assets and backups. Production location remains open. See [staging](infra/staging.md), [hardening](infra/hardening.md), and ADRs [0014](adr/0014-infrastructure-cost-migration.md) and [0015](adr/0015-owner-decisions-2026-11-22.md). The stack is a fresh deployment, not a Railway migration; `deploy-vm` deploys tested images. R2 has no object versioning, an accepted current-phase limitation; protected assets use encrypted checksum-pinned copies and WAL/base backup prefixes use bucket locks and expiry.

OpenRouter is the only live model provider, with live features gated by promotion evidence (ADRs 0015 and 0019). Google and Apple SSO use Clerk; email codes remain first-party (ADR 0016). The Lesson test defaults on with an explicit kill switch (ADR 0017). The daily check-in credits, example Courses and first-run tour are separate work in progress.

Live operations still have specific prerequisites, including scoped backup identities and key custody, provider credentials, evaluation evidence and production decisions; consult the relevant issue, ADR and runbook instead of treating all open issues as owner-blocked. See [CI](ci.md) for validation and [the data inventory](data-inventory.md) for retention and erasure.
