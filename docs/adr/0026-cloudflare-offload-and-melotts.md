# ADR 0026: Cloudflare offload and the MeloTTS decision

- Status: Accepted for implementation planning (controller decision, 2026-11-24). Narration default change pending owner approval.
- Date: 2026-11-24
- Change: [`cloudflare-offload`](../../openspec/changes/cloudflare-offload/proposal.md)

## Context

The owner selected Workers AI MeloTTS as the intended default Narration provider on 2026-11-24. Cloudflare retired `@cf/myshell-ai/melotts` on 2026-11-20 and documents `410` for calls. Aura costs per input character and has different language coverage. The current Workers AI Free quota would cover roughly two to three standard Aura-2 lessons a day before headroom. [Model contract](https://developers.cloudflare.com/workers-ai/models/melotts/), [retirement](https://developers.cloudflare.com/changelog/post/2026-11-05-planned-model-deprecations/), [Workers AI pricing](https://developers.cloudflare.com/workers-ai/platform/pricing/).

## Decision

- **Narration.** Keep ElevenLabs as the default while the owner chooses a replacement. Add an opt-in Workers AI provider for allowlisted Aura models. Language eligibility, model promotion and a free neuron budget gate each attempt. Fall back to budgeted ElevenLabs and then captions. ElevenLabs continues to serve entitled Plus/Pro voice choices and pinned Character dialogue. This supersedes the single-vendor assumption in ADR 0009. Keep the existing player, asset/credits guarantees and Pricing config approval process.
- **OpenRouter egress.** Put authenticated AI Gateway in front of OpenRouter where configured. Disable logs and caching per request for Learner content. Cache only explicitly eligible, identical non-personal requests. Fall back directly to OpenRouter within the existing attempt budget. Maintain STT ZDR and the handling policy in ADR 0021, and use direct transcription until the Gateway path is verified.
- **Public reads.** Default to Hyperdrive for eligible anonymous Marketplace browse and Course pages after local workerd conformance with the migrated PostgreSQL projections and `duolify_edge_ro`. Use `PUBLIC_READ_MODE=go_cache` for rollback and fall back to trusted Go reads on database failures. The edge serves fresh cache hits with no Go call and gates them with the Worker release flag and rate limiter. Go owns viewer authorization, moderation, the authored demo fallback and share proof and revocation. Hosted Tunnel, TLS, Access and Free-account validation are separate owner release gates. [W3 design](../../openspec/changes/cloudflare-offload/design.md#w3-public-reads-and-bot-protection) · [Role runbook](../infra/edge-public-read.md).
- **Chat.** Use an authenticated per-session Durable Object for Tutor and network-backed Guided chat, with hibernating WebSocket, SSE and the Engine path as successive fallbacks. Default `CHAT_EDGE_ENABLED` to `false`. Keep Go as the only authority for quote, reserve, settle and release. Store authoritative history and drafts in Postgres with existing backup and erasure.
- **Origin.** Postgres, Redis, River, the credits ledger, Go API core and lesson generation stay on Hetzner. This keeps data and generation jobs within the existing transaction, recovery and privacy boundaries and keeps the private Engine and core progress/auth routes near their dependencies.

## Consequences

Each edge path has a bounded origin/provider fallback. Validate parity and fallback locally, and require separate authorization for enablement and resource provisioning. Use Cloudflare Free allocations only. A paid plan, overage or other new spend requires explicit owner approval. The [design](../../openspec/changes/cloudflare-offload/design.md) records vendor limits, the W3 conformance gate and the open spend questions.
