## Why

Every setup, creation and settings task in the client is still a form, with chat bolted on in two places (setup, course brief) that spends model calls on questions we already know how to ask. The owner (2026-11-23) wants form filling removed: every use case becomes a chat where a friendly guide asks scripted questions answered by inline widgets (choices, checkboxes, clock, ...), with free-text questions allowed in between, and a default animated mascot (a green robot guide) the Learner can customise.

## What Changes

- New `guided-chat` capability: a deterministic **guided chat** pattern (declarative script, widget set, editable answers, resume, offline), a free-text-in-between router (answer / question / FAQ / command) and the retirement of form-fill screens, with an inventory of every current form mapped to a script or an explicit "stays" decision.
- New `mascot` capability: an original green robot guide (working name **Tinko**), eight states with triggers, lightweight SVG/CSS animation, default placement in chat, onboarding, the framed lesson player, empty states, celebrations and errors, and Learner customisation (recolour, accessories, or replace with one of their own Characters) over a part schema shared with `custom-characters`.
- Replace AI-driven setup chat and course brief chat questions by scripts (zero AI cost). AI remains only for free-text questions (quoted) and outline change proposals (already quoted).
- Add per-Learner server-side guided-chat drafts (retained 30 days) and a mascot preference stored in Learner settings.
- Mascot drawings and an animation demo are committed under `docs/design/mascot/`; `docs/design/brand.md` is amended (it currently says "avoid mascots").
- **BREAKING (client UX)**: setup form, brief form, SignIn form, character form and similar screens are removed after a one-release flag rollback window. API contracts for the underlying endpoints are unchanged.

## Capabilities

### New Capabilities

- `guided-chat`: script format, widgets, answer editing, resume/offline, free-text routing, credits and guardrails, accessibility, form inventory.
- `mascot`: character, states, animation rules, placement, customisation, storage, shared part schema, accessibility.

### Modified Capabilities

- `learner-profile`: setup chat becomes a scripted guided chat; profile edit and reminders become scripts; settings list rule.
- `course-creation`: brief chat becomes a scripted guided chat; drafts may be server-backed; brief chat no longer AI-priced.
- `analytics`: guided chat and mascot event catalogue and funnel metrics.
- `data-backup`: guided chat drafts and mascot preference in backup, retention, erasure and restore drill.
- `accounts`: sign-in as guided chat with real inputs.
- `design-system`: brand exception for an original mascot, chat widget a11y contract.
- `lesson-player`: corner companion in the framed player, hidden in immersive unless speaking.

## Impact

- Client: new `features/guided-chat` (engine, widgets, scripts, FAQ), `features/mascot` (rig component, states, picker); migration of the forms in the inventory; player chrome slot for the companion.
- API: draft endpoints (`/v1/me/guided-drafts/*`), intent route endpoint, `mascot` field on settings; existing submit endpoints unchanged. New migration for drafts.
- Engine: cheap intent classifier task and golden cases; setup/brief chat RPCs become unused (removal is a later matching API+engine+proto change).
- Proto: unchanged in this change.
- Infra: none beyond nightly dump coverage of one new table.
- ADRs: relies on [0004](../../../docs/adr/0004-auth.md), [0005](../../../docs/adr/0005-client-and-lesson-player.md), [0006](../../../docs/adr/0006-llm-providers-and-evals.md), [0007](../../../docs/adr/0007-credits-and-payments.md), [0010](../../../docs/adr/0010-custom-characters.md), [0011](../../../docs/adr/0011-owner-decisions-2026-11-09.md), [0013](../../../docs/adr/0013-owner-decisions-2026-11-16.md), [0019](../../../docs/adr/0019-live-promotion-evidence-in-the-engine.md). An ADR for the mascot and scripted-chat decisions is written in task group 0.
- Coordination (out of scope, parallel): `simplify-ux-gestures-hotkeys`, `custom-characters` rewrite, `prompt-skill-routing-and-guardrails`.
