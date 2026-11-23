# ADR 0024: Scripted guided chat and original mascot

- Status: Accepted
- Date: 2026-11-23
- Change: `openspec/changes/conversational-flows-and-mascot`

## Context

The owner asked (2026-11-23) to remove form filling for all use cases and replace it with a chat: fixed questions answered with chips, checkboxes, a clock and similar widgets, with free text allowed in between, and a green animated robot guide that is customisable. About 35 screens are forms today. E13-03 already provides the accessible thread, composer and polite log.

## Decision

- Questions come from deterministic, versioned scripts bundled in the client (typed TS modules, JSON Schema checked in CI). No model call, no Credits. Branches use a closed expression language (`eq`, `in`, `answered`, `not`), never `eval`.
- Widgets are real native controls: single chips, multi-select checkboxes, time, date, stepper, toggle, short and long text, file or image, rating, confirm card, and secure email, one-time code and password inputs (labelled, with `autocomplete`, inside a one-field `<form>` so password managers and OTP autofill keep working).
- Answered steps collapse to editable Learner bubbles. An edit re-evaluates branches, keeps still-valid later answers and lists what changed. Nothing is submitted before the confirm step.
- No step counters. Progress is an `aria-hidden` ambient bar.
- Drafts live server-side in `guided_chat_drafts` (16 KB cap, 30 days idle, `If-Match`), with a local copy for offline. Secure steps are `persist:false`; pre-auth sign-in is memory only. Drafts are in backups, export and erasure.
- Final submits use the existing endpoints unchanged; the server still validates. Settings, admin forms, notes, tutor chat, search and lesson tests stay as they are.
- Free text never mutates a script. Order: guardrail, deterministic prefilter, intent route (`answer`, `question`, `faq`, `command`, `off_topic`). Until the guardrail change lands, only the no-AI fallback runs: prefilter, then buttons "Use as my answer", "Ask the guide", "Skip". Destructive and paid steps can only be confirmed with the button.
- Each flow ships behind a per-script flag with the old form kept for one release (rollback), then deleted.
- One original green robot mascot, never a borrowed character. Its name is a working name held only in the message catalogue.

## Consequences

Zero AI cost for scripted steps, offline operation, one pattern for all task flows. Long scripts risk feeling long, so scripts stay near 10 steps. One new table and three endpoints; no proto change. The classifier route and mascot component land in later changes.
