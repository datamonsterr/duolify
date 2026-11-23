# Architecture decision records

| ADR | Title | Status |
|---|---|---|
| [0001](0001-monorepo-layout.md) | Single monorepo, one directory per deployable | Proposed |
| [0002](0002-service-architecture.md) | Go API in front, Rust engine behind gRPC | Proposed |
| [0003](0003-data-stores.md) | Postgres, Redis, S3-compatible storage, Postgres job queue | Proposed |
| [0004](0004-auth.md) | First-party auth in the API | Accepted |
| [0005](0005-client-and-lesson-player.md) | Tauri 2 + React client, Motion HTML slides in sandboxed iframes | Proposed |
| [0006](0006-llm-providers-and-evals.md) | LLM providers, gated generation, golden set on Hugging Face | Proposed |
| [0007](0007-credits-and-payments.md) | Credits ledger, daily free credits, subscriptions, no credit rewards | Accepted |
| [0008](0008-ci-and-deploy.md) | Two-tier CI on Namespace runners, Railway deploy | Accepted |
| [0009](0009-narration-elevenlabs.md) | Slide narration with the ElevenLabs API | Accepted |
| [0010](0010-custom-characters.md) | Custom characters with a parametric SVG rig and bounded personas | Accepted |
| [0011](0011-owner-decisions-2026-11-09.md) | Owner decisions for the E1–E17 breakdown | Accepted |
| [0012](0012-interim-defaults-2026-11-13.md) | Interim defaults for open human gates and integration flow | Accepted |
| [0013](0013-owner-decisions-2026-11-16.md) | Owner decisions for the remaining human gates | Accepted |
| [0014](0014-infrastructure-cost-migration.md) | Infrastructure migration off Railway to Hetzner Helsinki behind Cloudflare | Accepted |
| [0015](0015-owner-decisions-2026-11-22.md) | Owner decisions of 2026-11-22: routing, Hetzner, R2, domain, narration, CI | Accepted |
| [0016](0016-clerk-for-sso.md) | Clerk for SSO (Google, Apple, future social providers) | Accepted |
| [0017](0017-lesson-test-on-by-default.md) | The lesson test is on by default; the flag is a kill switch | Accepted |
| [0018](0018-structured-outputs-and-screen-reasoning.md) | Structured outputs for JSON tasks, reasoning none for safety screens | Proposed |
| [0019](0019-live-promotion-evidence-in-the-engine.md) | Live promotion evidence embedded in the engine | Proposed |
| [0021](0021-voice-input-data-handling.md) | Voice input: audio processed in memory and never stored, ZDR providers only | Proposed |
| [0022](0022-security-headers-and-csp.md) | Security headers and CSP: edge-set web policy, sandboxed lesson frame with its own policy, API middleware | Accepted |
| [0024](0024-scripted-guided-chat-and-mascot.md) | Scripted guided chat and original mascot | Accepted |
| [0025](0025-prompt-skills-router-and-input-guard.md) | Prompt skills, skill router and consolidated input guard | Accepted |
| [0026](0026-cloudflare-offload-and-melotts.md) | Cloudflare offload and the MeloTTS decision | Accepted for implementation planning |

New ADR: copy the newest file, increase the number, keep it under one page.
