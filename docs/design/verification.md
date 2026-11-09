# E13 verification and handoff

Verified 2026-11-08. Open [prototype/index.html](prototype/index.html) directly from disk; it contains all HTML, CSS and JavaScript and needs no server or network. Start lesson → play/replay/seek → Take lesson test → choose “Earth stays still” → read correction → answer the daylight question → resolve the later retry variant → Finish. Credits opens an illustrative shop; no money, AI requests or analytics are involved.

The prototype's four navigation shortcuts are **review controls**, not the full product navigation. The shipping sidebar, five mobile tabs, More destination, route guards and deep-link behavior are specified in [information-architecture.md](information-architecture.md). The lesson/test use a focused shell. A single authored slide stands in for a full slide deck; narration is a silent clock, clearly labelled.

## Reproduction

From the repo root, with the pinned mise toolchain:

```sh
mise run client:install
mise exec node -- node docs/design/prototype/check.mjs
mise exec npm:@fission-ai/openspec@1.3.1 -- openspec validate app-design --strict --no-interactive
mise run ci:openspec
```

`ci:openspec` runs `openspec validate --all --strict --no-interactive`. The checker loads the workspace's existing `@playwright/test`; it adds no package dependencies. Chromium is required; when absent, use the existing `mise run client:e2e-setup` task. The HTML itself does not need any installed package. Optional `--tokens` or `--browser` isolates checks.

## Results

| Check | Result |
| --- | --- |
| `app-design` strict validation | Passed |
| All OpenSpec specs and changes, strict | 15 passed, 0 failed |
| JSON/CSS/prototype semantic color parity | All 19 colors per mode matched; sRGB components agree with hex |
| Shared dimensions and timing | Spacing, radius, size and duration token declarations match JSON, CSS and prototype |
| Contrast calculation fixtures | Black/white 21:1, identical color 1:1, symmetry passed |
| All permitted foreground/background pairs | 42 passed; minimum text 5.57:1, minimum non-text UI 3.57:1 |
| Offline Chromium via `file://` | Passed at 320, 390, 768 and 1440px; 1000px viewport height |
| Page errors, console errors and network attempts | Zero across browser acceptance runs |
| Learn path, player, test and shop reflow | No horizontal page overflow at each width |
| Playback | Explicit Play, Pause, replay to zero, speed, mute, caption toggle, bookmark |
| Lesson test | Wrong original → intervening topic → paraphrased retry; wrong retry → further variant; all-correct route; finish gated on empty queue |
| Tutor/note sheets | Keyboard trap, Escape, focus restoration, retained tutor draft and raw quick note |
| Payment boundary | Cancel and pending keep balances unchanged; no live checkout or credit grant |
| Appearance and motion | System dark token switch; reduced motion keeps the Earth diagram static while the timeline advances |
| 200% text size | Learn path has no horizontal page overflow at all four widths; bottom navigation clears the final action |
| Visual inspection | Desktop path/player and 390px light path/dark shop screenshots reviewed; screenshots were temporary review artifacts |

The browser contract was written before the prototype; its first run failed because the HTML did not exist. Token integration assertions and contrast unit fixtures followed before the implementation. Browser findings during implementation included an escaped-string syntax error and a legend locator mismatch; both were corrected before the passing run.

## Boundaries and remaining decisions

This is design evidence, not a complete WCAG conformance audit. Screen-reader testing, real device/Tauri behavior, RTL/translated copy review, all-screen 200% text testing, final logo/sound artwork and learner usability sessions remain for implementation. No app source, backend, proto, AI prompt, golden-set dataset, actual audio, payment service or runtime analytics was changed. Existing app-area suites and `engine:eval` were not run because their implementation did not change.

Pricing, pack sizes, allowance expiry/rollover, taxes and mobile billing are E12 decisions. Shared friend-day/freeze consumption is an E6 decision. Cross-locale narration/caption quality is an E14 validation need. Stale lesson-test/reward-credit/freeze-purchase spec passages require separate feature changes before integration; this restricted change records their precedence without altering them. See [brand.md](brand.md#open-design-questions) for design questions and the OpenSpec proposal for the runtime boundary.

All design sources, tokens, prototype and checker are Git-backed. Reopen resets the in-memory demo; there is no local storage, learner data or payment state to back up. Runtime implementations must preserve the capability-specific backup, analytics consent, golden-set, fallback and credit-release requirements described in the screen catalog.
