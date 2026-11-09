# E13: App design

## Why
The bootstrap client has useful course and player structure but no shared brand, accessible tokens or complete interaction contract. E13 gives implementation tickets one reviewable design baseline before app integration.

## What Changes
- Add original Duolify branding, light/dark design tokens, component states and responsive navigation in `docs/design/`.
- Describe every product screen, credit boundary and learner recovery state; demonstrate the core loop in a self-contained offline prototype.
- Add capability `design-system`: tokens, WCAG 2.2 AA, reduced motion, dark mode, breakpoints, localization and UI analytics naming.
- Document source drift: the owner's explicit all-correct/retry-variant and effort-earned-freeze rules take precedence over older lesson-test, credits, gamification and friend-streak spec passages. This change does not migrate those capabilities.

## Revision 2026-11-09
- Re-theme to a playful green, Duolingo-style look with original branding; update `tokens.json`, `theme.css`, the prototype's token values, contrast evidence and `check.mjs` pairs together. New roles: `secondary`, `info`, `credit`, `streak`, `lime`.
- Replace the onboarding form and topic form with guided chats (quick replies, follow-ups, live summary or course brief, explicit confirm); the form stays as a fallback and the profile editor.
- Make the sample outline editable before the outline job: reorder by drag or move buttons, rename, remove, add, undo, and chat change proposals shown as a diff.
- Design every surface desktop first with the same functions on mobile; add the canvas source under `docs/design/canvas/`.
- Open pricing questions for E12: a free clarifying chat and about 5 credits per outline chat change are proposals only.

## Impact
Documentation and a static review artifact only. No app wiring, runtime dependencies, API/proto changes, new AI calls or real payment flow. E1–E9/E12/E14 implementation tickets consume the design. Exact commercial prices remain E12 decisions.

## Data backup and analytics
Design sources are backed up by Git; the prototype keeps ephemeral in-memory demo state only. No production data or PostHog requests are created. The delta defines a UI event catalog and privacy rules for future implementation.

## Model reliability
No prompts, model routing or generated content change. The prototype contains authored fixtures. Existing AI golden sets, engine eval gates, timeout/fallback and refund rules remain requirements of the implementing features.
