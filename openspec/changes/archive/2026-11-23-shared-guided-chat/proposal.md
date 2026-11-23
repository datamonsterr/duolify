# E13-03: Shared guided chat and editors

## Why
Setup, course brief and outline chat need the same accessible interaction primitives before their feature integrations ship (#33).

## What changes
Implement C14–C17 with authored fixtures: guided thread/composer, editable summary, outline editor and proposal diff, native modal sheet and responsive panel. Add labelled form and feedback primitives. Match Components, S01, S04 and S09 canvas boards using the token contract.

## Impact
Client only, with a separate fixture entry; no application shell changes. API, engine, proto and infra unchanged. Relies on ADR 0004 (auth context), ADR 0005 (Motion HTML) and ADR 0007 (credit boundaries). No new paid operations or AI behavior.

## Data, analytics and reliability
Source and fixtures are Git-backed. Drafts are ephemeral and reset on reload. Stable UI events use the existing consent/UUID boundary; no learner text. Existing feature eval/fallback gates remain with E1/E2 consumers.
