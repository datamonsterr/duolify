# Simplify UX: less copy, gestures on phones, hotkeys on desktop

## Why
Owner request (2026-11-23): "1. Less explain text, simple text. Using color, icons, layout, position that fit human psychology to design better UI UX for mobile 2. using more swipe to close sidebar, skipping in video, etc. For desktop using hotkeys with standard conventions." Today screens explain themselves with paragraphs (for example `onboarding.intro`, `review.text_help`, `onboarding.privacy`), controls rely on reading, and only the immersive player has gestures and a few `Escape` handlers. Learners should act without reading, with one thumb on a phone or the keyboard on a desktop.

## What Changes
- Add a **copy budget** per screen type, a plain-words rule, a screen-by-screen copy audit, and a lint/test that fails when a screen exceeds the budget or adds user-visible strings outside `messages.ts`. Explanations move to toasts, first-use coach marks and long-press tooltips. English and Arabic (RTL) stay complete.
- Add a **visual language** rulebook built on named HCI principles (Fitts, Hick, recognition over recall, Gestalt, colour semantics, Jakob, feedback time, serial position) with measurable rules, an icon-only allowlist, a bottom tab bar, bottom sheets on phones, safe-area insets and a one-handed reach map.
- Add a **gesture layer** for phones: swipe to close sidebars, sheets and the answer panel (RTL mirrored), swipe down to dismiss, horizontal swipe and double tap on the lesson stage, long-press for 2x, pull down to exit immersive mode, protected system-gesture margins, thresholds, cancellation and haptics. Every gesture has a visible or keyboard alternative; hints appear once as coach marks.
- **MODIFIED** `immersive-lesson-mode` "Answer panel over the Slide": owner request (2026-11-23), "duplicate the close button and collapse button in top right corner of the lesson sidebar, just remove both and allow user to swipe left to right from the edge". Both header buttons go; the panel closes by swipe, Escape, tap outside or a hidden focusable close control.
- Add a **hotkey layer** for desktop: video-player conventions in the lesson player, app-wide shortcuts, IME-safe Enter/Shift+Enter, a `?` help overlay, `aria-keyshortcuts`, Tauri menu accelerators that match.
- **MODIFIED** `immersive-lesson-mode`: double tap now seeks 10 seconds (it was previous/next Slide); horizontal swipe stays previous/next Slide; adds long-press 2x and pull-down exit.
- Add content-free analytics events (`gesture_used`, `hotkey_used`, `coach_mark_shown`, `coach_mark_dismissed`, `toast_shown`) and the adoption metrics.

## Capabilities

### New Capabilities
- `ux-simplification`: copy budget, plain-words rule, copy audit and lint, transient explanation primitives (toast, coach mark, tooltip), HCI-based visual rules, phone layout rules.
- `touch-gestures`: phone gesture vocabulary, thresholds, conflict resolution, alternatives, haptics and coach marks.
- `keyboard-shortcuts`: desktop hotkey catalogue, lesson-player map, input and IME guards, help overlay, accessibility and menu consistency.

### Modified Capabilities
- `immersive-lesson-mode`: "Touch gestures and lock" is replaced (double tap, long-press, pull-down exit, margins).
- `analytics`: new UI events and adoption metrics.

## Impact
Client only: `apps/client/src/**` (all features' copy, shell, lesson-player, chat, notes), `apps/client/src-tauri` (menu accelerators, optional haptics plugin), a new lint under `mise run client:lint`, new e2e specs. No API, proto, engine or database change. No AI is involved: no prompts, models, golden-set cases, fallbacks or Credits change.

## Data backup
No new data tables. New per-Learner settings, stored with the Learner profile, included in account export, removed on erasure and covered by the Postgres backup and restore drill: `haptics` (exists), `gesture_hints_seen` (set of coach-mark ids), `hotkey_hints_seen`, `swipe_navigation` (on/off, default on), `single_key_shortcuts` (on/off, default on). Toast text and coach-mark copy are static client strings.

## Related changes (out of scope, written in parallel)
- `conversational-flows-and-mascot` replaces forms with chat and a mascot. This change defines the copy budget those chat turns follow (one short line per turn) and the coach-mark primitive the mascot may host.
- `custom-characters` and `prompt-skill-routing-and-guardrails` are untouched; no AI copy or routing is specified here.
- `immersive-overlay-polish` (another agent: translucent sidebar, small bubble ask/note buttons, icon-only sidebar, explanations via toasts) was not present on `origin` when this was written (no branch, PR or change folder). This change is written to be compatible: it requires an icon-only sidebar, bubble actions and toast explanations and does not specify their pixel design. Reconcile at merge: if that change lands first, its sidebar requirements win on layout and this change adds only gestures, copy and hotkeys.
