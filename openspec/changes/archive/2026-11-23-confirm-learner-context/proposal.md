# Confirm learner context (E1-03, #42)

## Why
Learners need an optional, editable setup form and profile editor before their context is persisted or used by AI. Guided orchestration belongs to E1-11.

## What Changes
- Add authenticated current-profile reads, export and confirmed partial writes, with explicit Skip and configurable Unicode text limits.
- Extend current learner_profiles with goal, topic, level, daily minutes/XP, reminder preferences and setup completion; preserve unrelated preferences.
- Add responsive setup/form summary and profile editing with draft handoff, disclosure, retry and clear controls.
- Catalogue API-owned analytics, add PostHog privacy checks and extend encrypted backup restore/deletion evidence.

## Impact
API accounts, client accounts, migrations/sqlc, backup fixtures and data inventory. No new AI behavior, provider, pricing or engine contract.
