# ADR 0013: Owner decisions for the remaining human gates

- Status: Accepted (owner, 2026-11-16)
- Date: 2026-11-16

## Context

After all agent-buildable tickets merged, the open tickets waited on owner decisions (HITL) or credentials. The owner answered the decisions on 2026-11-16. Credentials and API keys follow separately; nothing goes live until they exist and the live gates are switched on.

## Decisions

| Gate | Decision |
|---|---|
| ADR 0012 | Accepted as written; its interim defaults are the v1 defaults. Live switches still need their own approval. |
| E12-09 #154 | `setup_chat` = 0, `course_brief_chat` = 0, `outline_chat_change` = 5 Credits, Daily free credits eligible, charged only for a valid delivered proposal; Apply/Discard/manual edits free. Recorded as `design_chat_decision: owner_approved` in a new Pricing config version. |
| E17-02 #40 (price) | `character_create` = 25 Credits, not Daily free eligible, in a new Pricing config version. Limits from `openspec/changes/custom-characters/design.md` stand (10 active, 5 creates/regenerations per day, 2 per minute). |
| E15-12 #37 (policy) | v1 sharing defaults stand: learner pays for missing lessons, no flow-back to the owner, no forked updates, public pages `noindex`. |
| E15-12 #37 (age) | Collect a birth date (at first publish if not known) and require 18+ to publish. Store only what the age check needs; include it in account export and deletion. |
| E15-12 #37 (audio) | Published snapshots may reuse the owner's narration audio for learners of shared copies. The owner confirms the ElevenLabs plan permits redistribution. |
| E15-12 #37 (moderation) | The owner is the sole moderator and copyright contact for v1 (grant via `moderator_grants`). |
| E15-13 #147 | v1 ships link sharing only: `MARKETPLACE_ENABLED` stays off and push stays in-app only. Revisit after link-sharing feedback. |
| E7-05 #24 | Stripe web checkout only, USD. Merchant of record: the owner as an individual, Stripe Tax on; refunds through the existing E7-10 flow. Apple/Google billing (E7-12, E7-13) parked. |
| E7-05 #24 (catalogue) | The packs and plans in `docs/business/monetization.md` are approved as the Stripe catalogue; E12-08 maps them once keys exist. |
| E13-05 #35 | The current original placeholder mark, green theme and tokens are approved as the v1 brand. No UI sound cues ship until assets exist; optional feedback settings stay. |

## Still open

Credentials and accounts: Stripe keys and products, ElevenLabs key and live-voice approval (E14-05 #116), Hugging Face dataset and eval secrets (E10-01 #25), backup storage and keys (E11-02 #27), environment secrets and rollout (E11-07 #54), live auth credentials (E1-10 #48). Default-cast character art (E17-02 #40) still needs approval.

## Consequences

Agent tickets unblocked: Pricing config version for the approved prices; birth-date age source for publishing; narration reuse in shared copies; E13-06 installs the approved placeholder brand. Live activation stays behind the credential gates above.
