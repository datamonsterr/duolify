# Select entitled voices with explicit synthesis quotes (E14-06)

## Why
E14-02..04 narrate every Lesson in one default voice. Plus and Pro include curated voices at the same synthesis rate, so a Learner needs to choose one. Switching voice is a new, optional synthesis: it must be entitled, disclosed with a quote, consented to, and never charged by a setting change alone. Replaying the already generated voice stays free.

## What Changes
- Approved voice set in config (`services/api/internal/narration/voices.json` and the Engine's `voices.json`): the default voice plus test fixtures (ADR 0012 E14-05). Logical keys only, no vendor ids or prices; prices come from the Pricing config. Fixture voices are exposed only when the service is built for tests.
- Entitlement: curated voices need the verified funded-plan benefit "Curated voices at the same synthesis rate" (Plus, Pro) from Postgres subscription terms (E7-07).
- API: `GET /v1/me/narration/voices`, `GET /v1/lessons/{lesson}/narration/voice` (state of the chosen voice), `POST …/narration/voice/quote` (characters, credits, Pricing version, voice-config hash; no reservation) and `POST …/narration/voice` (consent: accepts the quote, stores `learner_profiles.narration_voice`, queues synthesis that is bound to the accepted quote). A switch to a voice whose assets are all stored needs no quote and is a free `narration_cache_hit`.
- `slide_narrations` is keyed by lesson, slide and voice, so the old voice's row and asset stay referenced; the player serves the chosen voice when ready, else the stored one.
- Global narration-off (Learner setting `narration` or `NARRATION_ENABLED`) skips enqueue, engine calls, quotes and charges. Insufficient audio credits disable only audio (`disabled`/`insufficient_credits`) and the player offers Retry and Add credits without blocking text or the test.
- Client: voice panel in the player with the new character quote, Confirm/Decline, unentitled state (upsell shown only to plans without "No upsell banners", firing `credits_upsell_shown`), recovery states.
- Analytics: existing `narration_cache_hit`, `narration_muted`, `credits_upsell_shown`; no new events or text properties.
- Data: migration `20261113181405_narration_voices`, backup fixture, restore check, `docs/data-inventory.md`, privacy classification. Golden-set `lesson_narration` approved-voice and locale alignment rows with recorded fixture `evals/fixtures/narration.voices.json`.

## Impact
services/api (narration, pricing quotes, player, httpapi, migrations, backuptest, accounts privacy, pricingserver), engine-core voices config and tests, evals fixtures, apps/client player and narration, docs.
