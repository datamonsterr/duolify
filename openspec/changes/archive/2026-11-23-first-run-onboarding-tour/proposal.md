# First-run welcome and guided tour

## Why
New learners land without a clear next step, and setup chat is hidden in Profile. Give them a short, optional welcome and a friendly introduction to real controls.

## What Changes
- Offer a topic, existing setup chat, or available example courses, with Skip always available.
- Add a lazy, accessible spotlight tour on Learn; replay from Settings.
- Persist independent pending/skipped/completed welcome and tour states per learner, with an offline outbox and migration exemption for existing learners.
- Catalogue consent-aware, enum-only funnel events and cover persistence in export, erasure and backup classification.

## Impact
Client, accounts API, learner_profiles migration, design-system/learner-profile/analytics specs and mocked browser tests. Example endpoints use the agreed contract only. No engine, pricing, setup-chat internals or infrastructure changes; existing AI evaluation and fallback gates remain unchanged.
