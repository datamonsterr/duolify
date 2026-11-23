# Enforce the ElevenLabs monthly character budget

## Why
ADR 0015 puts narration on the ElevenLabs Starter plan: 40,000 characters per subscription month. Nothing stops live synthesis at the limit, so overage or hard failures would reach learners.

## What Changes
- Add `narration.Budget` (`NARRATION_MONTHLY_CHAR_LIMIT` default 40000, `NARRATION_BUDGET_STOP_PERCENT` default 95, `NARRATION_CYCLE_START_DAY` default 1). It applies only when `NARRATION_LIVE_ENABLED=true`.
- Count provider usage from Postgres: `narration_assets.characters` created in the cycle (one row per content hash, so shared and reused audio adds nothing) plus completed `narration_regenerations.characters`.
- Before any quote, reservation or live request, admit fresh items while the cycle total stays at or under the stop line. Refused items park as `disabled` with `narration_budget_exhausted` (captions only, no charge). Operator regeneration stays queued.
- Emit `narration_budget_exhausted` (counts only) and an ERROR log line `narration monthly character budget exhausted` for alerting.
- Not included: reading live remaining quota from `GET /v1/user/subscription`. It needs `ELEVENLABS_API_KEY` in the API service, which only the engine holds today; the Postgres count with a stop margin is the guard.

## Impact
services/api (narration, analytics catalogue, cmd/api), `.env.example`, docs/data-inventory.md. No migration, no proto change.

## Data and reliability
- Backup: no new tables; counts come from tables already in the nightly dumps.
- Analytics (PostHog): `narration_budget_exhausted` with `characters_used`, `characters_limit`, `characters_requested`, `stop_percent`, `items_denied`.
- Reliability: fail closed on a count error (River retries the job); concurrent runs can overshoot by their own size, which the 5% margin (2,000 characters) absorbs.
