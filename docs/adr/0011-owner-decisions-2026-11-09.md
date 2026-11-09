# ADR 0011: Owner decisions for the E1–E17 breakdown

- Status: Accepted (owner, 2026-11-09)
- Date: 2026-11-09

## Context

The ticket breakdown (`docs/epics/tickets.md`) found decision tickets that blocked most implementation work. The owner answered them on 2026-11-09. Values that live in the Pricing config stay changeable by a new config version.

## Decisions

| Ticket | Decision |
|---|---|
| E1-01 | ADR 0004 accepted: first-party email one-time-code sign-in, then Google and Apple OAuth. OTP mail via **Resend**; local and CI use a log-only mailer. Tokens on device via the Tauri keyring (OS keychain). |
| E1-05 | Launch defaults: learners 16+; GDPR-style data export and account deletion; AI-processing disclosure in onboarding; general backups kept 14 days, financial ledger backups 35 days. Legal review is still required before public launch. |
| E10-01 (gate only) | AI tickets develop and gate on golden-set fixtures committed in `evals/fixtures` (offline evals, mock provider). The live Hugging Face eval gate is enabled once the owner provisions the dataset and secrets; E10-01 blocks only that switch. |
| E4-06 | When a retry pool is exhausted, reuse the pool (reordered) for free. No on-demand paid AI variants at launch. |
| E6-05 | A shared Friend-streak day is each friend's own local date; it counts when both complete a Lesson on their own local date and those dates overlap in real time. Each friend sees their own local date; rollover is decided by the later day-end (spec rule). |
| E12-04 | Adverse pricing changes need 72 hours notice. The owner is the sole `pricing_admin` at launch; until a second admin exists the owner may approve adverse versions after the notice window. SKUs below 70% margin always block activation. |
| E16-04 | Steering limits: 500 characters per instruction, 20 active, 4000 total, 10 adds per course per hour. Regenerating an already generated lesson and rejoining a published course stay deferred. |
| E16-06 | `outline_replan` costs 55 credits in a new Pricing config version; a successfully produced proposal is charged even if discarded. |
| E17 | ADR 0010 accepted: original parametric SVG Character rig, no AI image generation in v1. Default limits from `openspec/changes/custom-characters/design.md` accepted. |

## Defaults chosen during implementation (pending owner review)

Not owner decisions; chosen by the orchestrator so work could continue. Change them by a new Pricing config version or ADR.

| Ticket | Default |
|---|---|
| E12-02 | Action quotes are independent of wallet reservations. The initial quote TTL is 10 minutes; provider/model rates come from the business-model cost envelope; the logical narration voice key is `default`. Quote policy is an immutable extension owned by each Pricing version. Quoting never enables a model or calls a provider. |
| E1 fixes | Local Playwright servers default to ports 4010 (web) and 4011 (API); the disposable API fixture uses 100000/hour for each email-code request and verification bucket so a parallel full-suite run cannot exhaust production throttles. |
| E1-07 | Client-side export encryption uses PBKDF2-SHA256 (310,000 iterations) and AES-256-GCM with a user-provided passphrase (minimum 12 characters). Server export payload expiry window is 7 days. Account deletion severs personal references and anonymizes credit entries and action quotes to a SHA-256 pseudonymous owner reference (retaining financial entries at least 7 years per spec), revokes all sessions, cascades immediate erasure of personal profile and auth records, creates durable backed-up tombstones replayed on restore via `ApplyDeletionTombstones`, enqueues PostHog person deletion requests, and clears device storage/caches. |
| E1-11 | Setup chat is platform-paid 0 credits in development config version usd-dev-unapproved (approved=false) and disabled in production until E12-10 approval; setup chat uses Haiku bracket with 2000 token input and 1000 token output ceiling; vague answers are limited to at most one follow-up per question; unconfirmed transcripts/summaries are client session drafts only and are never persisted server-side; profile persistence occurs only on explicit learner confirmation via canonical POST /v1/me/profile. |

## Still open (owner provisioning)

Live credentials and accounts: E1-10 (Resend, Google, Apple), E7-05/E7-12/E7-13/E7-14 (billing), E10-01 dataset and secrets, E11-02/E11-07 (backup keys, environment secrets), E12-08 (launch catalogue), E13-05 (brand assets), E14-05 (voices), E15-12/E15-13 (marketplace policy and pilot), the `character_create` price and final default-cast art (E17).
