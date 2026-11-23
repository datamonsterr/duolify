# Persist settings and analytics consent

E1-06 connects the settings screen to the existing learner profile record and makes analytics consent a live boundary. Existing locale, time zone, daily-goal, reminder and analytics opt-out fields remain the source of truth. Add persisted appearance, motion, audio, captions and text-size preferences without collecting preference values in PostHog.

## Area impact

- Client: replace the Settings scaffold with persisted, accessible controls and consent-aware analytics.
- API: extend the authenticated learner-profile endpoint and validate preference updates.
- Engine and proto: no changes.
- Infra: no new service or secret; the existing nightly PostgreSQL dump covers the extended profile row.

Relies on [ADR 0011](../../../docs/adr/0011-owner-decisions-2026-11-09.md), which accepts E1-05's learner privacy, backup and retention defaults.

## Scope

- Persist settings in `learner_profiles`, which is included in nightly PostgreSQL dumps.
- Extend the authenticated profile API and settings UI with accessible save/error states.
- Apply consent changes immediately to client capture, clear queued events on opt-out, and retain server-side opt-out checks.
- Keep PostHog derived and non-blocking; emit only allowlisted events and UUID/common properties.

## Out of scope

Account export/deletion flows, delayed credit-timezone effects (E7), and chat-based profile updates (E1-11).
