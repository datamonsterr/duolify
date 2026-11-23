# Record owner-approved design chat pricing

## Why
ADR 0013 (2026-11-16), E12-09 #154, approves the existing interim design chat prices. The approval must live in a new immutable document without changing historical versions or enabling live features.

## What Changes
- Add a separate `usd-design-chat-owner-approved-2026-11-16` fixture with `design_chat_decision: owner_approved`, the approved 0/0/5 Credit prices, Daily free eligibility, existing usage envelopes and explicit settlement/free-edit policy.
- Preserve `outline_replan=55` and its envelope from the E16 fixture; leave the separate character pricing version independent.
- Reuse draft → submit → owner approve/schedule → activation. Do not seed an approved database row or change production defaults: these would bypass the recorded owner approval required by the gate.
- Verify production decision/owner checks, unchanged development/test behavior, forward rollback, immutable history and financial restore.
- Keep existing canonical pricing analytics and privacy rules; no new events or learner text.
- Retain the new policy JSON with pricing versions/audit in existing nightly encrypted financial backups and seven-year history (35-day snapshot rotation). No new tables or learner fields.
- No prompts, models, provider calls or evaluation changes. Existing feature golden sets, fallbacks and live promotion gates still apply.

## Impact
API pricing schema/config, fixtures, executable acceptance tests, pricing README and data inventory. No live activation, migrations, proto or client changes.
