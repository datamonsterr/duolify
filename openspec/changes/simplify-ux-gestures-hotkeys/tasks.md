## 1. Copy audit and lint (size M)
- [x] 1.1 E2E: failing Playwright `copy-budget.spec.ts` counts visible strings per screen at 390 by 844 in `en` and `ar` (dir rtl) and checks heading, label, caption limits (all screens listed in the audit) (done: `apps/client/e2e/copy-budget.spec.ts` (10 routes x en/ar at 390 by 844, baseline `e2e/copy-budget.baseline.json`, `COPY_BUDGET_UPDATE=1` lowers it); also runs under `client:e2e-localization`)
- [x] 1.2 Integration: lint fixtures for literal-outside-catalogue, missing `ar` or `en-XA` key, over-budget string by role, denylist word, Flesch-Kincaid over 7, baseline raise (done: `scripts/copy-lint/copy-lint.integration.test.ts` (7 fixtures))
- [x] 1.3 Unit: word and character counters, role-from-key-suffix, FK grade, denylist, `data-user-content` exclusion (done: `scripts/copy-lint/lib.test.ts` (10 tests) and `catalogue_parity.test.ts` (fails on a missing ar key))
- [x] 1.4 Code: `apps/client/scripts/copy-lint.ts` (TypeScript AST scan) wired into `mise run client:lint`; baseline file with per-file counts that only ratchets down; `mise run client:copy-audit` generating `docs/design/copy-audit.md` with current and target counts per screen (done: `scripts/copy-lint.ts`, `copy-lint.config.json`, `copy-baseline.json`; tasks `client:copy-lint` (in `client:lint-web`), `client:copy-baseline`, `client:copy-audit`)
- [x] 1.5 Commit the first audit (exact counts replacing the grep estimates in design.md) and the plain-words denylist (done: `docs/design/copy-audit.md` committed; design.md counts replaced; denylist in `copy-lint.config.json`)

## 2. Toast, coach mark and tooltip primitives (size M)
- [ ] 2.1 E2E: toast within 100 ms, auto-dismiss 4 s, swipe dismiss, Esc, undo restores state; coach mark once per id across reload; tooltip on long-press and hover
- [ ] 2.2 Integration: `gesture_hints_seen` and `hotkey_hints_seen` stored with Learner profile, export, erasure, backup coverage
- [ ] 2.3 Unit: queue (max 2), live-region roles, reduced motion, 200% text
- [ ] 2.4 Code: `Toast`, `CoachMark`, `Tooltip` in `features/design`, catalogue ids, analytics `toast_shown`, `coach_mark_shown`, `coach_mark_dismissed`
- [ ] 2.5 API settings fields `swipe_navigation`, `single_key_shortcuts`, `gesture_hints_seen`, `hotkey_hints_seen` with `haptics` (matching proto and API changes only if the profile schema requires them)

## 3. Visual rules and phone layout (size M)
- [ ] 3.1 E2E: reach test at 390 by 844 (primary action in lower 40%, targets at least 44 by 44, 8 px gaps), greyscale state test, safe-area test
- [ ] 3.2 Unit: `data-action-level` counter (1 primary, 2 secondary), icon-only allowlist check (icon-only controls outside the list fail)
- [ ] 3.3 Code: bottom sheet component replacing centred modals below 48rem, tab bar insets, shared icon set and label rule
- [ ] 3.4 Docs: add the human-factors rules and reach map to `docs/design/design-system.md` and the component catalogue

## 4. Gesture layer (size L)
- [ ] 4.1 E2E (touch emulation): swipe-close sidebar/answer panel/sheet in LTR and RTL, short drag cancel, flick, double-tap seek and accumulation, swipe Slide, long-press 2x, pull-down exit, edge margin ignored, second finger cancel, `swipe_navigation` off, lock
- [ ] 4.2 Integration: gesture layer with scrolling sheet content, axis lock, pointercancel, orientation change
- [x] 4.3 Unit: thresholds module (56 px, 800 ms, 1.5 ratio, 40%, 0.5 px/ms, 400 ms, 96 px, 24 px margin) and the conflict priority table (swipe-close thresholds done in `swipeClose.ts`; seek, long-press and pull-exit still open)
- [ ] 4.4 Code (answer panel swipe-to-close, grab handle and tap-outside done in `useSwipeClose`; sheets, sidebar, toasts, double-tap seek, long-press, pull exit open): shared `useGesture` layer migrating `immersive/gestures.ts`; swipe-close for sidebar, sheets, answer panel, toasts; double-tap seek change (MODIFIED requirement), long-press 2x, pull exit; visible seek buttons in the control layer
- [ ] 4.5 Haptics: verify the Tauri mobile haptics API in current docs, wire to the `haptics` setting, `navigator.vibrate` fallback
- [ ] 4.6 Analytics: `gesture_used` including `undone_within_2s`; coach marks for the five gesture ids
- [ ] 4.7 Reconcile with `immersive-overlay-polish` if it has landed (sidebar layout and toasts)

## 5. Hotkey layer and help overlay (size L)
- [x] 5.1 E2E: help dialog and phone sheet, focus return, tooltips, Esc layering, input and IME guards, and layout-dependent question mark. The focused player integration fixture covers key dispatch and the single-key off switch until the player adopts the hook.
- [x] 5.2 Integration: registry versus handlers versus help groups versus `aria-keyshortcuts`; TypeScript and Rust tests for the shared native menu accelerator table.
- [x] 5.3 Unit: matcher (`code` versus `key`), guards (target type, modifiers, `isComposing`, repeat), focused-button Space rule, and composer Enter rules.
- [x] 5.4 Code: typed shortcut registry, `useHotkeys`, help overlay, tooltips, Tauri menu, and migration of handlers in available files. The per-Learner setting is included in account export and the Postgres restore drill, and removed on erasure.
- [x] 5.5 Analytics: `hotkey_used` uses registry IDs and honours opt-out. Text-field interactions emit no hotkey event.

Follow-ups after the reserved files are available

- Wire player + immersive handlers, player control shortcuts and end-to-end player scenarios. Adopt shared Escape and composer Enter handling in the reserved panels.
- After #372 merges, mount `SingleKeyShortcutSetting` and the shortcut help control in `Settings.tsx`.
- Move `features/hotkeys/messages.ts` into the shared message catalogue when the localization files are available.

## 6. Per-screen passes (one PR each; sizes S to M)
Each pass: failing budget and layout test, move strings to the catalogue, cut copy to budget, move explanations to toast, coach mark or tooltip, remove its baseline entry, update the audit row, attach review screenshots.
- [ ] 6.1 Onboarding and welcome (M; coordinate with `conversational-flows-and-mascot`)
- [ ] 6.2 Learn path and courses (M)
- [ ] 6.3 Lesson player, framed and immersive, plus lesson test (M)
- [ ] 6.4 Tutor chat and answer panel (M)
- [ ] 6.5 Quick notes and bookmarks (S)
- [ ] 6.6 Credits, pricing and paywall (S; keep cost and refund disclosures)
- [ ] 6.7 Settings, accounts and privacy (M; include the new settings toggles)
- [ ] 6.8 Sharing, marketplace, social and steering (M)

## 7. Design review and accessibility (size S)
- [ ] 7.1 Screenshots at 390 by 844, 844 by 390 and 1280 by 800, EN and AR, light and dark for the player and one other screen, under `docs/design/reviews/ux-simplify-<date>/`
- [ ] 7.2 axe scan with no serious or critical findings, keyboard-only walk-through, TalkBack and VoiceOver check of the alternatives to each gesture
- [ ] 7.3 Metrics dashboard for gesture and hotkey adoption, mis-swipe rate, coach-mark completion and time-to-first-action before and after
- [ ] 7.4 Sync the delta specs and archive the change
