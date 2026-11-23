# Guided onboarding setup chat (E1-11, #150)

## Why
Learners need a friendly conversational setup experience that guides them through optional onboarding questions (goal, background/level, daily time/XP goal, reminders, time zone), asks a clarifying follow-up only for vague answers, and maintains an editable live summary. Confirmation writes only to canonical profile persistence (E1-03), while skip or assistant failure gracefully falls back to the equivalent form with collected answers intact.

## What Changes
- Add additive `SetupChat` RPC and additive fields to `LearnerProfile` in `proto/duolify/engine/v1/engine.proto`, generated Go bindings, and Rust engine implementation.
- Implement engine-core setup chat logic: single follow-up bound, skipped fields handling, summary extraction, bounded failure handling.
- Add `setup_chat` versioned pricing key in an explicitly unapproved development config (cost 0, platform-paid) while keeping approved launch pricing unchanged and disabling setup chat in production without an E12-10 approved config.
- Add `POST /v1/me/setup-chat` API endpoint with auth, quote checking (zero learner credits, platform caps), engine client invocation, and opt-out analytics emission.
- Add client setup chat component integrating `GuidedChat` and `LiveSummary`/`ResponsivePanel` matching canvas board `S01-Welcome.dc.html`, supporting desktop panel / mobile sheet and confirm step, full keyboard/focus/reduced motion/light-dark, editable summary lines, retry/form fallback, and explicit confirmation before persistence.
- Add Golden-set cases in `evals/fixtures/` and threshold validation for `setup_chat`.
- Update `docs/data-inventory.md` and record implementation defaults in `docs/adr/0011-owner-decisions-2026-11-09.md`.

## Impact
- `proto`: Additive `SetupChat` RPC and additive fields on `LearnerProfile`.
- `engine`: `setup_chat` task in `models.toml`, implementation in `engine-server` and `engine-core/src/accounts/`.
- `api`: `POST /v1/me/setup-chat` endpoint, engine client method, pricing integration, analytics emission (`$ai_generation`).
- `client`: `SetupChat` component, routing in `/profile/setup` and "Update by chat" in `/profile`, desktop/mobile responsive parity with S01.
- `infra`: none (no new storage or infrastructure).
- Relies on: ADR 0004 (auth context), ADR 0005 (Motion HTML), ADR 0006 (LLM providers & evals), ADR 0007 (pricing and credits), ADR 0011 (owner decisions).
