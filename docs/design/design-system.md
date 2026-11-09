# Duolify design system

This is the E13 design contract, not a client migration. Start with [brand.md](brand.md), then use [tokens.json](tokens.json), [theme.css](theme.css), [screens.md](screens.md) and the [offline prototype](prototype/index.html). The scaffold's sidebar, path and player become shared components in later tickets. The scaffold's hearts counter is excluded.

**Theme revision (2026-11-09).** The owner chose a playful, Duolingo-style look: green primary actions, chunky 3D buttons, rounded heavy type and a zig-zag learn path. It replaces the earlier ink-violet palette. Role names are unchanged, so components keep their token references; values, font and path-node size changed, and new roles (`secondary`, `info`, `credit`, `streak`, `lime`) were added. The full screen and state set is drawn in the [Duolify App Design canvas](https://claude.ai/artifact/FggicArBXWkRVSfQnk7Ukn). The prototype's layout still predates this revision; only its tokens were updated.

## Token contract

The JSON follows the [DTCG 2025.10 format](https://www.designtokens.org/tr/2025.10/format/), a W3C Community Group format rather than a W3C Recommendation. Tokens use `$type`/`$value`; colors are opaque sRGB objects, dimensions have value/unit, durations use milliseconds and easing uses cubic Bézier arrays. `color.light` and `color.dark` are explicit mode groups. A consumer selects one group; it must not blend modes. Elevation references the light edge as its canonical base; the semantic CSS mapping uses the active mode's edge.

The [Tailwind v4 theme snippet](theme.css) uses `@theme inline` aliases for runtime semantic variables, following the [official theme guidance](https://tailwindcss.com/docs/theme). Keep it unwired until a client integration change. For example, `bg-surface text-text`, `bg-primary text-on-primary` and `rounded-duo-md shadow-press` refer to this contract. No external font or CDN is required. All visual decisions use tokens; authored instructional diagram geometry, percentages, 1px hairlines and platform safe-area insets are documented geometry exceptions. Do not invent colors for individual screens.

## Color roles

| Role | Light | Dark | Use |
| --- | --- | --- | --- |
| canvas | #F7F9F4 | #131F24 | Page canvas |
| surface | #FFFFFF | #1C2B31 | Cards, fields, sheets, captions |
| raised | #DDF1FF | #26383F | Selected navigation, selected choice, quiet panels |
| text | #2B2B2B | #F1F7F2 | Main text on neutral surfaces |
| muted | #555555 | #B8C7C9 | Secondary text; never reduce opacity |
| primary | #1F7A0D | #6CD43A | Main actions, START node, unit banner |
| primary-hover | #1A6B0A | #86E05A | Action hover |
| primary-edge | #145208 | #3E8F1B | Decorative solid button depth, no text |
| on-primary | #FFFFFF | #0E2606 | Primary action labels only |
| secondary / on-secondary | #1677C9 / #FFFFFF | #4DA6F0 / #0A1F33 | Secondary filled actions (Ask tutor, Copy link) |
| secondary-edge | #0F5A99 | #2B78BC | Depth for secondary buttons, no text |
| info / info-bg | #0D5592 / #DDF1FF | #9FD3FF / #173247 | Selected tab, retry-queued, job status, freeze notice |
| accent / on-accent | #FFC233 / #4A2E00 | same | Completed node, XP, daily goal, current badge |
| credit / credit-bg | #5B2BB5 / #F0E7FF | #D2BEFF / #2E2347 | Credits balance, paid gates, shop context |
| credit-icon | #7B3FE4 | #B08CFF | Gem icon and credit fills, no small text |
| streak / streak-bg | #A44E00 / #FFF1D1 | #FFB56B / #3E2F12 | Streak counts and streak cards |
| streak-icon | #FF8A00 | #FF9E3D | Flame icon, no text |
| lime / lime-edge | #58C322 / #46A01A | #6CD43A / #3E8F1B | Progress fills and illustration only, never under text |
| success / success-bg | #1E6E0C / #E3F8D3 | #9BE07A / #1A3A12 | Correct and complete, with checkmark/text |
| danger / danger-bg | #B3261E / #FDECEC | #FF9B95 / #4A1E1E | Correction and errors, with text |
| warning / warning-bg | #704400 / #FFF1D1 | #F5CF8A / #3E2F12 | Offline, waiting to sync, with text |
| line | #7A7A7A | #7F9499 | Meaningful borders and control boundaries |
| focus | #1677C9 | #7CC4FF | 3px focus ring, 3px surface-colored offset |

Use only the foreground/background pairs in the contrast table below. A feedback banner has its matching semantic foreground; its secondary body uses that same foreground. Primary-filled cards use on-primary for every label. The bright lime green is decoration only: it never carries text, and white text always sits on `primary`, not lime. Do not put muted text or status colors on arbitrary surfaces. Decorative card and divider borders may use a lighter #E3E3E3 because the `line` role marks meaningful control edges. Decorative diagrams use no essential color-only encoding. Disabled controls keep readable text and add an unavailable label and dashed outline rather than fading opacity.

## Contrast evidence

Ratios are computed from linearized sRGB relative luminance with `(Llighter + .05)/(Ldarker + .05)` using [WCAG's minimum contrast guidance](https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum.html). Every text pair below passes 4.5:1, including captions and disabled labels. `line` and `focus` rows are non-text UI only and pass 3:1. Large text is permitted at 3:1 but uses the stronger text pairs here. Values are rounded for display; checks compare unrounded values.

| Mode | Foreground / background | Hex pair | Contrast | Minimum |
| --- | --- | --- | --- | --- |
| light | text / canvas | #2B2B2B / #F7F9F4 | 13.36:1 | 4.5:1 |
| light | muted / canvas | #555555 / #F7F9F4 | 7.03:1 | 4.5:1 |
| light | primary / canvas | #1F7A0D / #F7F9F4 | 5.15:1 | 4.5:1 |
| light | line / canvas | #7A7A7A / #F7F9F4 | 4.05:1 | 3:1 |
| light | focus / canvas | #1677C9 / #F7F9F4 | 4.39:1 | 3:1 |
| light | text / surface | #2B2B2B / #FFFFFF | 14.16:1 | 4.5:1 |
| light | muted / surface | #555555 / #FFFFFF | 7.46:1 | 4.5:1 |
| light | primary / surface | #1F7A0D / #FFFFFF | 5.46:1 | 4.5:1 |
| light | line / surface | #7A7A7A / #FFFFFF | 4.29:1 | 3:1 |
| light | focus / surface | #1677C9 / #FFFFFF | 4.65:1 | 3:1 |
| light | text / raised | #2B2B2B / #DDF1FF | 12.20:1 | 4.5:1 |
| light | muted / raised | #555555 / #DDF1FF | 6.43:1 | 4.5:1 |
| light | primary / raised | #1F7A0D / #DDF1FF | 4.70:1 | 4.5:1 |
| light | line / raised | #7A7A7A / #DDF1FF | 3.70:1 | 3:1 |
| light | focus / raised | #1677C9 / #DDF1FF | 4.01:1 | 3:1 |
| light | on-primary / primary | #FFFFFF / #1F7A0D | 5.46:1 | 4.5:1 |
| light | on-primary / primary-hover | #FFFFFF / #1A6B0A | 6.66:1 | 4.5:1 |
| light | on-accent / accent | #4A2E00 / #FFC233 | 7.74:1 | 4.5:1 |
| light | success / success-bg | #1E6E0C / #E3F8D3 | 5.66:1 | 4.5:1 |
| light | danger / danger-bg | #B3261E / #FDECEC | 5.72:1 | 4.5:1 |
| light | warning / warning-bg | #704400 / #FFF1D1 | 7.45:1 | 4.5:1 |
| light | on-secondary / secondary | #FFFFFF / #1677C9 | 4.65:1 | 4.5:1 |
| light | info / info-bg | #0D5592 / #DDF1FF | 6.63:1 | 4.5:1 |
| light | credit / credit-bg | #5B2BB5 / #F0E7FF | 7.15:1 | 4.5:1 |
| light | streak / streak-bg | #A44E00 / #FFF1D1 | 5.11:1 | 4.5:1 |
| dark | text / canvas | #F1F7F2 / #131F24 | 15.47:1 | 4.5:1 |
| dark | muted / canvas | #B8C7C9 / #131F24 | 9.65:1 | 4.5:1 |
| dark | primary / canvas | #6CD43A / #131F24 | 8.90:1 | 4.5:1 |
| dark | line / canvas | #7F9499 / #131F24 | 5.28:1 | 3:1 |
| dark | focus / canvas | #7CC4FF / #131F24 | 8.96:1 | 3:1 |
| dark | text / surface | #F1F7F2 / #1C2B31 | 13.43:1 | 4.5:1 |
| dark | muted / surface | #B8C7C9 / #1C2B31 | 8.37:1 | 4.5:1 |
| dark | primary / surface | #6CD43A / #1C2B31 | 7.72:1 | 4.5:1 |
| dark | line / surface | #7F9499 / #1C2B31 | 4.58:1 | 3:1 |
| dark | focus / surface | #7CC4FF / #1C2B31 | 7.78:1 | 3:1 |
| dark | text / raised | #F1F7F2 / #26383F | 11.24:1 | 4.5:1 |
| dark | muted / raised | #B8C7C9 / #26383F | 7.01:1 | 4.5:1 |
| dark | primary / raised | #6CD43A / #26383F | 6.46:1 | 4.5:1 |
| dark | line / raised | #7F9499 / #26383F | 3.84:1 | 3:1 |
| dark | focus / raised | #7CC4FF / #26383F | 6.51:1 | 3:1 |
| dark | on-primary / primary | #0E2606 / #6CD43A | 8.56:1 | 4.5:1 |
| dark | on-primary / primary-hover | #0E2606 / #86E05A | 9.88:1 | 4.5:1 |
| dark | on-accent / accent | #4A2E00 / #FFC233 | 7.74:1 | 4.5:1 |
| dark | success / success-bg | #9BE07A / #1A3A12 | 8.04:1 | 4.5:1 |
| dark | danger / danger-bg | #FF9B95 / #4A1E1E | 6.94:1 | 4.5:1 |
| dark | warning / warning-bg | #F5CF8A / #3E2F12 | 8.75:1 | 4.5:1 |
| dark | on-secondary / secondary | #0A1F33 / #4DA6F0 | 6.39:1 | 4.5:1 |
| dark | info / info-bg | #9FD3FF / #173247 | 8.36:1 | 4.5:1 |
| dark | credit / credit-bg | #D2BEFF / #2E2347 | 8.67:1 | 4.5:1 |
| dark | streak / streak-bg | #FFB56B / #3E2F12 | 7.45:1 | 4.5:1 |

Recompute with `mise exec node -- node docs/design/prototype/check.mjs --tokens`. All pairs are enumerated; no opacity, gradient or image background may be introduced under text without a new measurement. Captions sit on an opaque surface. Focus uses a surface gap so it is measurable even on a filled button. Chart marks and selected borders also need shape/labels.

## Typography and layout

Font stack: `"Nunito", ui-rounded, ui-sans-serif, system-ui, sans-serif`. Nunito (SIL Open Font License) is bundled with the app as self-hosted WOFF2 files for weights 600, 800 and 900 — no CDN or runtime font download; until it is bundled the stack falls back to the system rounded/sans font. Keep Unicode fallbacks for non-Latin scripts. Sizes are rem equivalents of the following pixels at a 16px root; use no fixed-height text containers.

| Token | Size / line height | Weight | Use |
| --- | --- | --- | --- |
| small | 12 / 18 | 800 | Eyebrow, metadata only |
| caption | 14 / 21 | 600 or 800 | Captions, help, nav |
| body | 16 / 24 | 600 | Reading, forms, choices |
| title | 20 / 23 | 900 | Card and section heading |
| heading | 28 / 32.2 | 900 | Screen title on mobile |
| display | 40 / 46 | 900 | Desktop screen title |

Body leading 1.5; heading leading 1.15. Caption text can be enlarged independently in the player. Tabular numbers for XP, credits and time. No all-caps sentences; use small eyebrows. Short button labels (one to four words) are uppercase with 0.8px tracking as part of the chunky button style; long or translated labels that wrap switch to sentence case. Limit reading lines to roughly 60–70 characters.

Spacing uses a 4px base: `space.0/1/2/3/4/5/6/8/10/12/16` = 0/4/8/12/16/20/24/32/40/48/64px. Cards use 24px padding desktop, 16px mobile; section gap 32px; related control gap 8 or 12px. Radius sm 8, md 16, lg 24, pill 999px. Elevation flat 0; tactile press 0 4px 0 primary-edge. Use a visible border for dialogs, avoiding ambiguous blur shadows.

Size tokens: target 44px, path node 72px, sidebar 224px, support rail 280px, reading column 680px, full shell 1280px, focused shell 1000px, dialog/toast maximum 600px, slide frame 340px (300px mobile), minimum timeline/note area 120px. Typography tracking: wide 1.2px, tight −0.8px. Modal backdrop opacity is 0.9. Below 48rem: one column, 16px gutters and a bottom tab bar with safe-area padding. At 48rem: sidebar + flexible main, 32px gutters. At 75rem: optional support rail. A 30rem optional breakpoint adjusts card columns, not navigation. Breakpoints are layout thresholds, not device detection. Fixed navigation must reserve its full rendered height, including wrapped translations and safe area. Focused player/test hide peripheral navigation but keep Exit and return context.

## Motion

| Token | Value | Use |
| --- | --- | --- |
| none | 0ms | Reduced motion, data replacement |
| press | 90ms | Button press/release, 3px vertical displacement |
| fast | 140ms | Color/focus-related hover feedback |
| normal | 240ms | Sheet entry, progress fill |
| slow | 420ms | Optional single completion accent |
| standard | cubic-bezier(.2, 0, 0, 1) | Most transitions |
| enter | cubic-bezier(.16, 1, .3, 1) | Sheet entry |

Never animate continuously outside instructional content. No flash, shake or confetti storm. Honor OS reduced motion and a learner setting; the more restrictive choice wins. Set transition durations to none, remove press displacement, sheet travel, automatic scrolling and decorative loops. Instructional Motion HTML displays a static meaningful frame plus a complete explanation; its fake/real narration clock can still play. Pause on opening a tutor/note sheet and when the document becomes hidden. Don't steal playback focus.

## Component inventory and states

All controls: default, hover, focus-visible, pressed, disabled/unavailable, pending and error where applicable. Target ≥44px; click semantics use buttons, navigation uses links. Native labels, keyboard support and text feedback are mandatory. Loading uses `aria-busy`; a polite live region announces only meaningful state changes. Primary actions describe their outcome.

| Component | Anatomy and behavior | State details |
| --- | --- | --- |
| Chunky 3D button | md radius, 16px inline padding, min 44px height, primary/on-primary, solid 4px edge | Hover primary-hover; press moves 3px and reduces depth; focus ring outside; pending “Preparing…” prevents duplicate submission; unavailable keeps label/reason, no opacity; reduced motion uses border/color only |
| Secondary / quiet / destructive button | Surface+text+line; quiet uses underlined text link for navigation; danger only for confirmed destructive actions | Clear focus; destructive dialog names the object, Cancel first; long labels wrap |
| Input / textarea / select | Visible label, help, counter if limited, inline error linked by describedby | Empty, filled, focus, invalid, read-only, saving; preserve draft on failure; never use placeholder as label |
| Card / course tile | Surface, line, lg radius, heading, description, one main destination | Selected adds text/border; pending skeleton has accessible label; unavailable course shows reason; titles wrap |
| Path node | 72px chunky circle with a 7px edge plus ordered lesson label, zig-zag offsets but linear DOM order | Completed: accent fill with check + “Completed”; current: primary fill, play icon, “START” bubble + “Ready”; needs credits: credit outline + gem icon; generating labelled spinner; locked padlock+prerequisite; failure Retry; offline-cached badge. No click target for unavailable lessons |
| Progress bar | Label and native progress value, raised track, primary fill | Loading indeterminate uses static label in reduced motion; completion text; no guessed generation percentage |
| Feedback banner | Semantic fg/bg pair, icon, heading, explanation, next action | Correct, correction, retry queued, offline, saved, freeze applied. No hearts or lives. Use `role=status`; assertive alert only for blocking validation |
| Toast | Surface/text, line, one sentence and optional Undo | Saves/bookmarks; 6s visible minimum, pause timeout on hover/focus; persistent alternative in screen; never sole location for errors |
| Sheet / dialog | Labelled heading, close control, body, explicit actions; full-width on phone | Open/pending/error; focus trapped and restored; Escape closes unless irreversible step is executing; avoid stacked dialogs |
| Chat bubble | Surface for tutor, raised for learner; author label and readable width | Composing, streaming with Stop, complete+Save quick note, stopped, failed before tokens, interrupted after tokens; no per-token live announcements |
| Audio controls | Play/Pause, Replay, Previous/Next, speed select, mute toggle, labelled timeline | Idle, playing, paused, ended, buffering, unavailable. All change slide and narration together; keyboard slider uses arrows/Home/End; disabled next says why |
| Captions / transcript | Opaque surface, text, resizable type, labelled toggle, full transcript disclosure | Shown/hidden; persists across slides; mute does not hide captions; audio failure keeps transcript; no fast word-by-word announcements |
| Test choices / queue | Fieldset+legend, radio choices, explicit Check answer, queue count | No selection, selected, submitting, correct, incorrect+explanation, retry later; answer choices lock after server grade; no answer revealed in production client payload before submit |
| Wallet / offer / daily meter | Separate daily, purchased and reserved amounts; local expiry; labelled radio offer cards | Zero, low, held reservation, expired; purchase pending, cancelled, failed, verified. Don't promise fixed feature counts as balances change; prices from E12 |
| Tabs / navigation / list row | Visible icon+label, current-page marker, proper link targets | Default/current, keyboard focus, long label; badge meaning includes text; overflow menus have explicit names |
| Toggle / checkbox | Native control + complete label and optional description | Checked/unchecked/focus/unavailable; appearance and consent changes never reset lesson state |

## Accessibility, localization and analytics

Use one h1 per screen, ordered headings, landmarks, a skip link, visible focus and no positive tabindex. Move focus to a new route heading; preserve scroll/focus when returning. Dialogs return focus to their trigger. Maintain content at 200% text and reflow at 320px/400% viewport zoom. Use visible labels for errors and `aria-describedby`; no drag-only actions or timed answer requirements. Distinguish disabled prerequisites from paid gates. Server-grade test answers; hold offline answers for verification before XP/streak/completion.

Strings receive stable keys such as `test.retry_queued`, `credits.daily_reset_at` and `player.take_test`; pass named arguments to complete translated messages. Use ICU plural messages and locale number/date/currency formatting. Reserve 40% expansion; allow multiline controls and RTL using logical properties. Do not concatenate translated fragments or use “today” without the learner's local day/time zone. Prototype strings are English authored fixtures, not a shipping localization catalog.

New UI events are catalogued in the change's `design-system` delta: `ui_screen_viewed`, `ui_navigation_selected`, `ui_control_activated`, `ui_sheet_opened`, `ui_purchase_option_selected`, `ui_checkout_requested`, `ui_recovery_selected`, `ui_test_retry_presented`, `ui_appearance_changed`. Use stable IDs/enums only. Existing product outcome events remain in `analytics/spec.md`; an API-owned outcome must not also fire from the client. Every future emitter needs catalog/type tests and consent handling. No answers, topics, chat, notes, profile text, slide HTML, email, URLs with private query strings or payment details in PostHog. The prototype sends nothing.

Design sources are Git-backed; prototype state is memory-only. Runtime PostgreSQL/object-storage backup and AI golden-set/timeout/fallback behavior belong to implementing capability changes. Design mockups do not claim those systems exist.
