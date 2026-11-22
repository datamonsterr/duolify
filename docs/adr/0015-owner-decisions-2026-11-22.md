# ADR 0015: Owner decisions of 2026-11-22

- Status: Accepted (owner, 2026-11-22)
- Date: 2026-11-22

## Context

ADR 0014 moved infrastructure off Railway. This ADR records the owner's follow-up decisions on models, hosting, storage, domain, narration and CI.

## Decisions

| Topic | Decision |
|---|---|
| AI models | Per-task routing via OpenRouter, table to follow. ADR 0006 defaults stay until the routing table lands in `engine/config/models.toml`. |
| D1 hosting | Deploy fresh on Hetzner. No Railway data migration. E18-06 #237 and E18-09 #240 are closed as not planned; PRs #265 and #266 are closed. |
| Object storage | Cloudflare R2 for app assets and backups. This supersedes Hetzner Object Storage in ADR 0014. One R2 key serves both today, so the separate writer, expire and restore identities of E11-02 #27 are still open. |
| Domain | `duolify.online`, behind Cloudflare. |
| Narration (D6) | English. ElevenLabs voice Matilda (`XrExE9yKIg1WjnnlVkGX`), Starter plan (commercial licence, 40k characters per month). |
| CI | GitHub-hosted runners for all workflows (PR #270), replacing Namespace runners in ADR 0008. |

## Consequences

- The `hcloud` CLI is a mise tool for Hetzner provisioning.
- Backup and asset code targets R2 through its S3-compatible API; split R2 keys by role before live backups (#27).
- The 40k characters per month narration cap needs a usage guard before narration goes live.
