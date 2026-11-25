## Context

Today: `features/accounts/SetupChat.tsx` and `features/courses/CourseBrief.tsx` call Engine RPCs to ask onboarding and brief questions and fall back to forms; `OutlineChat.tsx` and `features/chat/TutorChat.tsx` are AI chats; about 35 other screens are forms (inventory in the `guided-chat` spec). `features/design/chat/GuidedChat.tsx` (E13-03) already provides the accessible thread and composer primitives and a polite log; this change builds on them rather than starting over. The owner asked (2026-11-23) to remove form filling for all use cases, fake the chat UI to ask fixed questions with widgets, allow free text in between, and draw a green robot mascot, animated, default but customisable. Product decisions were delegated ("You decide all"); they are recorded here.

## Goals / Non-Goals

- Goals: one chat pattern for every task-like flow; zero AI cost for the scripted part; same endpoints and server validation; accessible, resumable, offline-tolerant; an original, light, animated default guide the Learner can customise.
- Non-Goals: rewriting `custom-characters` (parallel), gestures and hotkeys (`simplify-ux-gestures-hotkeys`), the input guardrail and router internals (`prompt-skill-routing-and-guardrails`), admin pricing forms, lesson test answering, Tutor chat itself, free-form note capture, search boxes, and any proto change.

## Decisions

| Decision | Choice | Why (alternatives rejected) |
|---|---|---|
| Who asks the questions | Deterministic scripts, no model call | Zero cost, instant, testable, works offline. Rejected: model-asked questions (cost, latency, nondeterminism, needs form fallback). |
| Where scripts live | Typed, versioned TS modules bundled in the client with a JSON Schema checked in CI; the server never serves scripts, it only validates submitted values | Offline and i18n for free; no new definition API. Rejected: server-defined scripts (extra endpoint, cache and version skew). A `script_version` is stored with each draft so old drafts migrate or restart. |
| Widget set | Single-choice chips, multi-select checkboxes, time (clock), date, number stepper/slider, toggle, short text, long text, image/file pick, confirm card, rating, secure inputs (email, one-time code, password) | Covers every field in the inventory. Each is a real native control (see Accessibility), not a canvas. |
| Secure inputs | Real `<input>` elements (labelled, `autocomplete`, `inputmode`, `name`) wrapped in a one-field `<form>` rendered inside the Learner-side bubble slot | Password managers, OTP autofill and screen readers keep working. Secrets and codes are never echoed or drafted. Rejected: custom keyboards or masked divs. |
| Answers as bubbles | Answered steps collapse to the Learner's bubble; tapping or pressing Enter on it reopens that step's widget. Later answers stay if still valid under branching and revalidation, otherwise only the invalid ones are re-asked and listed | Matches chat feel, avoids losing work. Rejected: linear undo stack (loses later answers). |
| Progress | No step counters. Ambient progress is an `aria-hidden` thin bar plus the final summary card; the mascot says "last one" on the final step | Owner wants chat not a wizard; counters recreate the form feel. |
| Draft storage | Server-side per Learner and script, 16 KB max, 30 days idle retention, plus an in-memory/local copy for offline. Pre-auth sign-in keeps its state in memory only. Secure steps are `persist:false` | Resume across devices after restart; erasure/export/backup are straightforward (one table). Rejected: local-only (loses on reinstall, fails restore story); infinite retention (privacy). |
| Free text in between | Pipeline: guardrail (from `prompt-skill-routing-and-guardrails`) then deterministic prefilter (commands: back, skip, help, undo; parse as step answer) then a cheap intent classifier returning a closed enum `answer / question / faq / command / off_topic`; FAQ matches are answered from a static bundled FAQ for free; questions go to the tutor/mascot as a quoted AI answer; then the script resumes at the same step | Cheap and safe; the script is never mutated by text. Classifier failure falls back deterministically (below). |
| Credits and quotes | Scripted steps and FAQ are free. An AI answer shows the existing Action quote inline (tutor-chat price) and is covered by Daily free credits first; no auto-spend; declining returns to the step. Classifier calls are platform-paid, capped, and never charged to the Learner | Consistent with ADR 0007 and brief chat's platform-paid rule. |
| Rate limits | At most 10 free-text turns per minute and 40 per script run per Learner; over the cap the mascot offers the buttons only | Abuse and cost bounding. |
| Classifier fallback | If the classifier times out (3 s), errors or returns low confidence: when the text validates as an answer for the current step, treat it as that answer (shown as an editable bubble); else show three buttons "Use as my answer", "Ask Buo", "Skip" | Never blocks the flow, never invents an AI spend. |
| Destructive actions | Account deletion, data export and Character deletion use an explicit confirm card (named consequences, secondary Cancel as default focus, destructive button never auto-focused, password input if required today). Free text and command routing can never confirm a destructive step | Prevents misrouted intents causing damage. |
| Settings | Stay a settings list (toggles, selects, sliders): appearance, motion, narration, captions, text size, sounds, haptics, privacy toggles, voice choice, locale. Conversations are for tasks and first-time choices: onboarding (incl. reminder time and daily goal), profile edit, reminders edit via "Change my reminder" chat shortcut, and similar one-off wizards | Toggles are state, not form filling; a list is faster to scan and re-open. Rejected: chat for toggles (slower, hides state). |
| Admin forms | Pricing admin and other operator forms stay forms | Operators are a different audience; power-user density. |
| Endpoints | New draft and route endpoints only; all submit endpoints and their validation unchanged | Keeps contracts, tests and security review intact. |
| Form removal | Per-script flag `FEATURE_GUIDED_<SCRIPT>`; old form stays behind the flag for one release for rollback, then deleted. No user-visible "use a form instead" | Owner wants forms gone; flag is an operator rollback only. |
| Mascot origin | Original robot mascot; not based on any existing brand character. No owl, no Duo name, silhouette, face, pose or trade dress. Only the owner's brief intent is taken: a friendly, expressive green robot guide using Duolify's own tokens | Legal and brand safety (ADR 0010, brand.md). |
| Mascot name | Final name **Buo** (owner decision 2026-11-25). Earlier alternatives **Nodo** and **Glimmo** dropped. A web search (2026-11-23) found no well-known mascot with these names; nearest are "Tiko" (a STEM kit robot) and "Robot Tinka" (a TV prop), so a trademark check is an owner action | Short, pronounceable in EN and AR transliteration. |
| Mascot art | Hand-written SVG parts (head, visor, eyes, mouth, antenna, ear pods, body, arms, accent) coloured only by CSS variables bound to tokens (`lime`, `lime-edge`, `primary`, `primary-edge`, visor `#0E2606`, eye mint, accent `accent`); one warm accent only; rounded geometry; readable at 32 px | See `docs/design/mascot/`. |
| Mascot runtime | React component rendering inline SVG, CSS keyframes (WAAPI for sequencing), IntersectionObserver pause, `prefers-reduced-motion` static poses; total added JS+CSS and art at most 30 KB minified (12 KB gzip target) | No Lottie/Rive runtime; stays inside `bundle-budget.json`. |
| Mascot placement | Chat avatar and guide; onboarding; empty states; celebrations; errors; corner companion in the framed lesson player (outside the slide viewport, 48 px); hidden in the immersive player except while speaking a hint | Consistent presence without covering slides. |
| Mascot vs cast | The mascot is a client UI guide, not slide content, so it never appears inside generated Motion HTML slides unless the Learner selects it as a Character in the Cast (ADR 0011/0013 consistency). Tutor chat persona stays the Cast main guide; the mascot is its avatar fallback only when the Cast main guide is the default | Keeps lesson generation, narration and grading independent. |
| Customisation | A "Your guide" picker (swatches from a contrast-validated palette allowlist, accessory enum, or "Use one of my Characters") stored as `mascot` in Learner settings | Free, no AI, shares the part schema with `custom-characters`. |
| Shared part schema | `RigPartSpec v1`: `rig` (`robot-v1` for Buo or a custom-characters rig id), `palette` (slots: `body`, `bodyEdge`, `trim`, `visor`, `eye`, `accent`, each a hex from an allowlist that passes 3:1 against both canvases), `accessories` (enum array, max 2), `states` (the eight names). The custom-characters expressions `idle, happy, sad, thinking, celebrate` map to `idle, happy, confused, thinking, celebrate`; the other three states (`listening, talking, sleepy`) fall back to `idle` for rigs that do not define them | Lets a Character stand in as the guide without a second system. Version number lives in this change; the `custom-characters` rewrite must adopt or extend it. |
| Language | EN and AR keys for every prompt, option and FAQ; layout mirrors in RTL, the mascot sits on the start edge and does not flip its art | Existing localisation rules. |

## Script format

Scripts are data. Branch predicates use a small closed expression language (`eq`, `in`, `answered`, `not`), never `eval`.

```ts
export const onboardingSetup: GuidedScript = {
  id: "onboarding.setup", version: 3, entryPoints: ["onboarding", "profile"],
  steps: [
    { id: "goal", widget: "chips+text", promptKey: "g.setup.goal.q", optionsKey: "g.setup.goal.opts",
      validate: { type: "string", max: 2000 }, skippable: true, persist: true, analyticsId: "goal" },
    { id: "level", widget: "single", promptKey: "g.setup.level.q",
      options: ["beginner", "intermediate", "advanced", "expert"], skippable: true, persist: true },
    { id: "reminder_time", widget: "time", promptKey: "g.setup.rem.q", when: { answered: "reminder_on" },
      validate: { type: "hhmm" } },
    { id: "confirm", widget: "confirm", summaryOf: ["goal", "level", "reminder_time"], submit: "PUT /v1/me/profile" },
  ],
};
```

Each step has a stable `id`, `widget`, i18n keys (EN+AR), validation mirroring the API limits, `skippable`, `persist` (false for secrets), optional `when`/`next` branches, an analytics id, and for the final step the existing endpoint it submits to. CI checks keys exist in both locales, validators match the API limits, and every branch target exists.

## Free-text routing

1. Input screening and router: reuse the guardrail from `prompt-skill-routing-and-guardrails`. Text is data. Blocked text gets a neutral mascot reply and the step re-asked.
2. Prefilter: slash-less commands ("back", "skip", "help", "start over") and exact FAQ keys match first. If the text parses as a valid answer for the current step (e.g. "7:30" on a time step), it is an answer.
3. Classifier (`guided_intent` task, cheap model): returns `answer | question | faq | command | off_topic` plus a FAQ id. Deterministic server-side validation of the closed enum.
4. Effects: `answer` fills the step bubble (editable); `faq` shows the static answer, free; `question` shows a quote card then a tutor/mascot answer (streamed, `talking` state), then "Back to your question" re-asks the step; `command` runs only non-destructive commands; `off_topic` gets a one-line redirect.
5. The same step is shown again after any detour. The transcript of detours is not part of the draft answers.

## API changes

- `GET|PUT|DELETE /v1/me/guided-drafts/{script_id}`: body `{script_version, step_id, answers (bounded JSON), updated_at}`; owner-only; 16 KB cap; 404 for other Learners; `If-Match` on `updated_at` to avoid cross-device clobber.
- `POST /v1/me/guided-chat/route`: `{script_id, step_id, text}` returns `{route, faq_id?, quote_required?}`; rate limited; no persistence of text.
- `PATCH /v1/me/settings`: adds the optional validated `mascot` object.
- All other endpoints (profile, brief confirmation, course creation, sharing, characters, privacy, credits) unchanged.

## Accessibility

The thread is `role="log"` with polite announcements of completed messages once per stable id (existing E13-03 behaviour); streaming text stays outside live regions. Each widget is a native control with a visible label or `aria-labelledby` to the mascot's question: radios for single choice, checkboxes for multi-select, `<input type="time">`/`date`/`number`, a `<fieldset>` with a legend for groups. The whole script is keyboard operable: Tab into the widget, arrows within groups, Enter to send, Esc to cancel an edit or close a sheet, `Shift+Tab` back to the composer. After each question, focus moves to the first widget control (not the bubble); after answering, focus returns to the next widget. No timers or auto-advance: nothing expires while the Learner thinks. Reduced motion removes mascot motion, typewriter effects and auto-scroll (jump-to-latest control instead). Touch targets 44 px, all colour pairs from the token contrast table.

## Offline

Scripts, FAQ and the mascot ship in the bundle. Answers save locally and to the draft when online. Steps whose submit needs the network or Credits (sign-in, purchase, outline proposals, AI questions) show the mascot `sleepy` with a non-blocking "You're offline" bubble and keep the answers; idempotent free submits (profile edit) queue and retry on reconnect.

## Risks / Trade-offs

- Long scripts feel like long chats → cap at about 10 steps per script, group optional steps behind "Anything else?".
- Chat is slower than a form for experts → settings stay lists; profile edit offers "jump to a step" chips; keyboard shortcuts come from the parallel gestures change.
- Drafts hold Learner text → same class as profile, 30 day retention, erasure cascade, never sent to analytics.
- Classifier mistakes → fallback buttons, destructive steps never reachable by text.
- Mascot looks childish → tokens only, restrained accent, no pet-name copy (brand voice rules); the Learner can hide the companion and pick a Character.
- Name collision → trademark check flagged to owner.

## Migration Plan

Ship the engine with the onboarding script behind its flag, A/B against the old flow (50/50 by Learner id for 14 days) to get completion and time baselines, then migrate flows in impact order (onboarding, course creation, sign-in, character, rest). Each flow keeps its old form behind the flag for one release, then deletes it. Rollback is the flag. Drafts table migration is additive.

## Open Questions

1. Final name: decided. Owner chose **Buo** (2026-11-25). Trademark check remains an owner-side action.
2. Whether the corner companion is on by default for Learners under the supported age range (proposed: on, hide in Settings).
3. Voice for the mascot: none in v1 (narration voices stay a lesson feature).
