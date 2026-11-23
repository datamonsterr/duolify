## Context
Owner request (2026-11-23): less explanatory text, plain wording, colour, icons, layout and position that fit human psychology on mobile; more swipes (close sidebars, skip in video); standard hotkeys on desktop. The owner delegated product decisions ("You decide all"); each is recorded below.

Current state (read from `origin/main`, 2026-11-23):
- Copy (exact, from `mise run client:copy-audit`, 2026-11-23): the shared catalogue `features/design/localization/messages.ts` holds 434 English ids (it includes the 37 onboarding ids; en and ar key sets match) and 1948 user-visible strings still sit as inline literals in 99 files: sharing 370, accounts 249, courses 169, lesson-player 142, social 136, pricing 111, gamification 109, credits 108, notes 103, steering 93, design 86, characters 82, narration 64, lessontest 48, LearnPath 37, shell 34, App 5, chat 2. The earlier grep estimates counted only JSX text; the AST scan also counts `aria-label`, `title`, `placeholder`, `alt` and label properties. Language selection is a runtime pseudo-localizer for `en-XA`, so only en and ar catalogues exist. The full per-screen table lives in `docs/design/copy-audit.md`.
- Gestures: only `features/lesson-player/immersive/gestures.ts` (double tap 300 ms/48 px, swipe 56 px/800 ms/ratio 1.5, region spread 6%). Double tap currently means previous/next Slide.
- Keyboard: ad hoc `Escape`/`Enter` handlers in `AdaptiveLayouts`, `ImmersiveHost`, `AnswerPanel`, `RegionSelector`, `LearnPath`; no registry, no help, no `aria-keyshortcuts`.
- Tokens (`docs/design/theme.css`): `primary` green, `success`, `warning`, `danger`, `secondary`/`info` blue, `credit` purple, `streak` amber, `accent` yellow, `lime` progress-only; `--size-target: 44px`. `mobileTabs` already holds four tabs. A `haptics` setting already exists in Settings.
- Dependencies: `immersive-overlay-polish` (translucent sidebar, bubble ask/note buttons, icon-only sidebar, toasts) is another agent's change; it was not on `origin` (no branch, PR or change folder) when this was written.

## Goals / Non-Goals
**Goals:** a measurable copy budget with a failing lint; named human-factors rules turned into testable layout rules; one gesture layer and one shortcut registry; every gesture and hotkey has a visible alternative; EN and AR (RTL) preserved; adoption metrics.
**Non-Goals:** chat-based forms and mascot (`conversational-flows-and-mascot`), characters, prompt routing or guardrails, new AI features, a command palette, remappable keys, new colours, new data tables, API or proto changes, changing lesson content or its generation.

## Decisions
| Decision | Choice | Why |
|---|---|---|
| Heading / label / caption limits | Heading at most 4 words and 24 chars; button and tab at most 2 words and 16 chars; caption at most 60 chars, 1 per card; toast 8 words; coach mark 10 words; error 12 words plus 1 action | Matches working-memory and glance-reading limits on a phone; numbers are testable; leaves room for the 40% expansion rule and Arabic at x1.3 |
| Strings per screen | 12 visible strings per screen, 8 per sheet; user and AI content excluded via `data-user-content` | Hick's law and scanning cost; content is not chrome |
| Reading level | Flesch-Kincaid grade at most 7 for strings of 5 words or more, plus a plain-words denylist (target grade 6) | Computable offline; strict grade 6 is noisy on very short strings |
| Where explanations go | Toast (what happened), one-time coach mark (how to use), long-press/hover tooltip (what an icon is); no persistent paragraphs | Transient help costs no space and shows at the moment of need; coach marks stored per Learner so they never repeat on another device |
| Enforcement | Lint in `client:lint` (AST scan, catalogue parity, budget, FK grade) plus Playwright visible-string budget per screen; a baseline file that only ratchets down | Catches regressions without blocking existing screens in one giant PR |
| Disclosures | Privacy, consent, credit costs, refunds, destructive confirms are exempt from the budget but stay reachable within 2 taps | Other specs require them; hiding them would break those requirements |
| Primary vs success colour | `primary` filled raised button marks the single primary action; `success` is a tint plus check icon for done/progress; `danger` red only for destructive/errors; amber (`warning`, `streak`) attention; `accent` yellow rewards; `credit` purple Credits; blue secondary/info | The theme's primary is green, so "green = success" is separated from "primary" by fill style and icon, not by a new colour (no new tokens) |
| Icon-only set | play, pause, previous, next, close, back, more, search, add, send, microphone, volume/mute, full screen, captions, settings, share, bookmark, home; all other icons carry a label of at most 2 words | These are learned from video, chat and OS apps; recognition beats recall for everything else |
| Phone navigation | Bottom tab bar (existing 4 `mobileTabs`, Learn first), bottom sheets instead of modals, no horizontal swipe between tabs | Fitts and Jakob; tab swipe collides with stage and carousel swipes and hides state |
| Panel close swipe | Toward the edge of origin, 40% travel or 0.5 px/ms flick, within 33 degrees, mirrored in RTL, content scroll wins | Direction matches the spatial metaphor; 33 degrees reuses the shipped 1.5 axis ratio; velocity makes quick flicks work |
| Double tap | Seek 10 s back or forward on the Lesson timeline, accumulating; horizontal swipe keeps previous/next Slide | Owner asked for skipping in video; 10 s is the YouTube/Netflix convention and Slides last a few seconds so Slide skip stays on swipe and buttons. This changes the shipped "double tap = Slide" and is a MODIFIED requirement |
| Mirroring | Panel swipes mirror in RTL; stage seek, Slide swipe and Left/Right keys do not | Media timelines and transport controls are not mirrored (platform convention); panels follow reading direction |
| Long-press | 400 ms hold on stage gives 2x while held, restores on release | Same as YouTube/Instagram; momentary mode cannot be left on by mistake |
| Pull down | Pull from stage at least 24 px below top, 96 px or 0.6 px/ms flick, exits immersive | Mirrors Android/iOS dismiss-by-pull conventions; 24 px keeps notification pull untouched |
| System gestures | 24 px (or larger platform inset) margin on all four edges ignored; no edge-open for sidebars | Never hijack back swipe or home indicator |
| Conflict priority | system, two-finger select, vertical, horizontal, long-press, double tap, tap; axis lock after 12 px; second finger cancels one-finger gestures | Deterministic and testable |
| Haptics | Light haptic on commit when the existing `haptics` setting is on; official Tauri haptics where available, else `navigator.vibrate(10)`, else none | Reuses an existing setting; plugin API must be verified against current Tauri docs at build time (not written from memory) |
| Hint delivery | One-time coach marks, per id, per Learner (server-stored), suppressed once the gesture is used | Hints never become permanent copy |
| Gesture off switch | `swipe_navigation` setting (default on) plus the lock control | Motor and accidental-touch safety; panel/sheet close stays |
| Standard keys | Space/K play-pause, Left/Right 5 s, J/L 10 s, Shift+Left/Right and PageUp/PageDown Slide, Home/End, 0-9 percent, Up/Down volume, M, C, F, < >, N note, B bookmark, R region, / ask, ? help, Esc | Follows YouTube/VLC conventions users already know; N and / per request; Slide stepping needs a modifier so the video-style arrows stay seeks |
| Command palette | Out of scope for v1 | Adds a surface, more copy and a search index for little benefit with 4 destinations; Ctrl/Cmd+1..4 covers navigation |
| Remapping | Not in v1; a single-key on/off switch instead | WCAG 2.1.4 is met by "turn off"; remapping needs UI, storage and conflict handling for little demand |
| Single-key scope | Active only when the player has focus and no text field is focused | WCAG 2.1.4 and screen-reader browse-mode safety |
| Key matching | Letters by `event.code`, symbols by `event.key` | Works on AZERTY and Arabic layouts |
| Enter | Enter sends and Shift+Enter newline with a fine pointer, Ctrl/Cmd+Enter sends, Enter is newline on touch; IME composition guard | Chat convention; soft keyboards have a send button |
| Desktop binds | Ctrl/Cmd+, and Ctrl/Cmd+1..4 only in the Tauri app via the native menu, not in the web build | Avoids browser tab shortcuts |
| Registry | One typed registry drives handlers, help overlay, `aria-keyshortcuts`, tooltips and menu table | Prevents drift; testable |
| Analytics | `gesture_used`, `hotkey_used`, `coach_mark_*`, `toast_shown`; hotkeys from text fields never recorded | Content-free and enough for adoption, mis-swipe and time-to-first-action |
| `immersive-overlay-polish` | Not contradicted; reconcile at merge, its sidebar layout wins | Not visible on `origin` yet; this change adds only gestures, copy and keys on top |

## Reach map and screen rules (phone, 390 by 844)
- Upper third: title (at most 4 words), back or close at inline-start, "more" at inline-end. Middle: content. Lower third: the one primary action, then secondary; tab bar below.
- Sheets: handle, 50% snap and full height, close at inline-start, content scroll first.
- Landscape lesson: stage full bleed, controls in the control layer, side insets honoured.

## Risks / Trade-offs
- [Copy cuts lose clarity] -> coach marks and toasts carry context; compare time-to-first-action and support contacts per screen before and after, revert a string if it rises more than 10%.
- [Double tap semantic change surprises existing users] -> one-time coach mark, visible buttons, setting to turn gestures off, release note.
- [Mis-swipes on small stages] -> 56 px, 33 degree and margin rules, reverse swipe undo, mis-swipe metric with a 5% trigger to retune.
- [Haptics plugin gaps on iOS web] -> visual feedback is primary; haptics optional.
- [Arabic strings longer or untranslated] -> x1.3 limit, key parity check, native review before release, `en-XA` pseudo-locale in tests.
- [Large PR surface across every screen] -> per-screen passes of one PR each, baseline ratchet so the lint can land first.
- [Screen reader conflicts with single keys] -> focus-scoped, settings off switch, visible controls.
- [Overlap with `conversational-flows-and-mascot`] -> that change owns chat turns and the mascot; this one supplies the budget (90-char turns) and the coach-mark primitive.

## Migration Plan
Land the lint with a baseline first (no behaviour change), then primitives (toast, coach mark, tooltip), the gesture layer, the shortcut registry, and then per-screen passes. Each pass removes its baseline entry. New settings default to the current behaviour except double tap (new seek meaning, announced by a coach mark). Rollback per pass by reverting the pull request; settings are additive.

## Open Questions
None for the owner; all product choices above were decided under the owner's delegation. Reconciliation with `immersive-overlay-polish` happens at merge time between the two authors.
