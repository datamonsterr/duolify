# ADR 0015: Owner decisions of 2026-11-22

- Status: Accepted (owner, 2026-11-22)
- Date: 2026-11-22

## Context

ADR 0014 moved infrastructure off Railway. This ADR records the owner's follow-up decisions on models, hosting, storage, domain, narration and CI.

## Decisions

| Topic | Decision |
|---|---|
| AI models | OpenRouter is the only provider. Every task uses `tencent/hy4-flash` with `z-ai/glm-5.3-flash` as fallback, no Claude models (details below). Supersedes the provider order and model defaults of ADR 0006. |
| D1 hosting | Deploy fresh on Hetzner. No Railway data migration. E18-06 #237 and E18-09 #240 are closed as not planned; PRs #265 and #266 are closed. |
| Object storage | Cloudflare R2 for app assets and backups. This supersedes Hetzner Object Storage in ADR 0014. One R2 key serves both today, so the separate writer, expire and restore identities of E11-02 #27 are still open. |
| Domain | `duolify.online`, behind Cloudflare. |
| Narration (D6) | English. ElevenLabs voice Matilda (`XrExE9yKIg1WjnnlVkGX`), Starter plan (commercial licence, 40k characters per month). |
| CI | GitHub-hosted runners for all workflows (PR #270), replacing Namespace runners in ADR 0008. |

## AI model routing

Every task runs on OpenRouter with one primary model and one fallback. No Claude models are used. Config lives in `engine/config/models.toml`.

| Tasks | Primary | Fallback | Reasoning (primary) |
|---|---|---|---|
| All tasks | `tencent/hy4-flash` | `z-ai/glm-5.3-flash` | see below |

- `none`: lesson, published_copy_lesson, note, setup_chat, course_brief_chat, outline_chat_change, chat (tutor).
- `low`: preview, outline, outline_replan, character_create. Planning needs some reasoning. (The four screens moved to `none` in ADR 0018.)
- GLM 5.3 Flash always runs with `low`, because its reasoning is mandatory and it cannot be switched off.

Live comparison by the controller (same 4-slide JSON lesson prompt, one sample each):

| Model and setting | Result | Output tokens | Cost | Time |
|---|---|---|---|---|
| Hy4 Flash, reasoning none | valid, accurate, detailed | 421 | $0.000063 | 3.3 s |
| Hy4 Flash, reasoning low | valid | 1,822 (1,247 reasoning) | $0.00026 | 8.9 s |
| GLM 5.3 Flash | valid, thin | 344 | $0.000031 | n/a |
| Claude Haiku 5.5 | valid | 795 | $0.00041 | n/a |
| DeepSeek V4.1 Flash | valid | n/a | $0.00039 | n/a |

Why Hy4 Flash: quality close to the paid options at near-GLM cost, and the cheapest cache read ($0.0015 per 1M tokens), which suits the context-heavy tutor chat. It has a single upstream provider (Tencent, fp8), so the fallback chain is essential. GLM is the cheapest output and the fallback.

OpenRouter prices per 1M tokens (input / output / cache read), checked 2026-11-22: `tencent/hy4-flash` 0.035 / 0.1405 / 0.0015; `z-ai/glm-5.3-flash` 0.0399 / 0.0784 / 0.03.

Engine behaviour:

- `OPENROUTER_API_KEY` is the only provider key. `ANTHROPIC_API_KEY` is no longer used; the direct Anthropic path is gone from live routing and the production config check requires only the OpenRouter key.
- OpenRouter's `reasoning` is a single request-level field, so it cannot differ per model inside a `models` array, and GLM cannot take `none`. The engine therefore tries the chain as sequential single-model requests, each with its own effort, instead of sending a `models` array.
- Requests send `provider.data_collection = "deny"` (no training on learner data) and `HTTP-Referer` / `X-Title` headers.
- Usage records the model that served the call (the response `model`). Reasoning tokens bill as output.

Accepted risk: Hy4 Flash and GLM output is not moderated by OpenRouter, so the safety screens (`steer_screen`, `character_screen`, `course_screen`, `screen_review`) now run on an unmoderated model. The owner accepted this. Screen logic and thresholds are unchanged; the screen golden-set cases must pass the live eval before launch.

Guardrails on OpenRouter (an allowlist of exactly these two models, no training, a daily budget) are configured outside the repo by the owner. They could not be created through the API on a personal account.

Cost notes: the old Haiku entries in `engine/config/prices.toml` were 10x too high and are fixed. Haiku and Sonnet entries stay because approved pricing versions and recorded eval data reference them.

Pricing versions: the approved versions (`usd-launch-2026-11-08`, `usd-quotes-2026-11-09`) embed Sonnet and Haiku rates and are not edited. Credit prices do not change, so only provider cost does. Quote and settlement code accepts usage only for the quoted model or its single configured fallback. Settlement now accepts usage from any model of the quote's ordered route (primary, then fallbacks) and charges the served model's rate. Migration `20261122120000_hy4_routing_pricing_unapproved.sql` adds the unapproved version `usd-hy4-routing-2026-11-22-unapproved`: every action on `tencent/hy4-flash` with `z-ai/glm-5.3-flash` behind it, rates in integer nanodollars per token rounded up (Hy4 35 / 141 / 2, GLM 40 / 79 / 30 for input / output / cache read), and credit prices, limits and every other term copied unchanged from `usd-character-create-2026-11-16`. Learner prices do not change, so the change is non-adverse and the 72-hour notice (`adverse_notice_hours`) does not apply. The owner must still approve it before live activation.

## Consequences

- The `hcloud` CLI is a mise tool for Hetzner provisioning.
- Backup and asset code targets R2 through its S3-compatible API; split R2 keys by role before live backups (#27).
- The 40k characters per month narration cap needs a usage guard before narration goes live.

## Amendment 2026-11-22: owner approved the Hy4/GLM quote policy

The product owner approved `usd-hy4-routing-2026-11-22-unapproved` ("Price approve"). Migration `20261123110000_hy4_routing_pricing_approved.sql` records it as the new immutable approved version `usd-hy4-routing-2026-11-22` (config identical except `version` and `effective_from`, quote policy verbatim) and activates it with the rows `ActivateDue` writes, so quotes name the route the engine serves and `POST /v1/courses/preview` stops returning 502. The unapproved row is untouched.

- Learner credit prices, packs, plans and limits are unchanged; the change is non-adverse.
- This approval covers the Hy4/GLM quote policy only. Any future learner-facing price change still goes through the owner approval flow and the 72-hour adverse-notice rule.
- The owner review trail (approved draft, submit and approve audit rows) is written only where an owner-approved draft already exists; `PRICING_OWNER_ID` is not stored in the repo or migration, and no gate in code is weakened.
