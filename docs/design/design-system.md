# Duolify design system

This is the E13 design contract, not a client migration. Start with [brand.md](brand.md), then use [tokens.json](tokens.json), [theme.css](theme.css), [screens.md](screens.md) and the [offline prototype](prototype/index.html). The scaffold's sidebar, path and player become shared components in later tickets. The scaffold's hearts counter is excluded.

## Token contract

The JSON follows the [DTCG 2025.10 format](https://www.designtokens.org/tr/2025.10/format/), a W3C Community Group format rather than a W3C Recommendation. Tokens use `$type`/`$value`; colors are opaque sRGB objects, dimensions have value/unit, durations use milliseconds and easing uses cubic Bézier arrays. `color.light` and `color.dark` are explicit mode groups. A consumer selects one group; it must not blend modes. Elevation references the light edge as its canonical base; the semantic CSS mapping uses the active mode's edge.

The [Tailwind v4 theme snippet](theme.css) uses `@theme inline` aliases for runtime semantic variables, following the [official theme guidance](https://tailwindcss.com/docs/theme). Keep it unwired until a client integration change. For example, `bg-surface text-text`, `bg-primary text-on-primary` and `rounded-duo-md shadow-press` refer to this contract. No external font or CDN is required. All visual decisions use tokens; authored instructional diagram geometry, percentages, 1px hairlines and platform safe-area insets are documented geometry exceptions. Do not invent colors for individual screens.

## Color roles

| Role | Light | Dark | Use |
| --- | --- | --- | --- |
| canvas | #F7F5F0 | #171420 | Page canvas |
| surface | #FFFFFF | #231F30 | Cards, fields, sheets, captions |
| raised | #EDE8FA | #302A42 | Selected navigation, quiet panels, path track |
| text | #242038 | #F6F1FF | Main text on neutral surfaces |
| muted | #5F596F | #C4BBD4 | Secondary text; never reduce opacity |
| primary | #5942B5 | #BCA6FF | Main actions and links |
| primary-hover | #483398 | #CDBEFF | Action hover |
| primary-edge | #392873 | #8270B7 | Decorative solid button depth, no text |
| on-primary | #FFFFFF | #22163E | Primary action labels only |
| accent / on-accent | #F4BE69 / #38250C | same | Current-step badge, optional highlight |
| success / success-bg | #216446 / #E5F3EB | #A1DDBB / #19392D | Correct and complete, with checkmark/text |
| danger / danger-bg | #A52D45 / #FBECEF | #FFABBC / #482332 | Correction and errors, with text |
| warning / warning-bg | #774617 / #FFF0D6 | #F1CD99 / #3D3020 | Offline, low credits, with text |
| line | #80768F | #9C90AF | Meaningful borders and control boundaries |
| focus | #5942B5 | #D9CBFF | 3px focus ring, 3px surface-colored offset |

Use only the foreground/background pairs in the contrast table below. A feedback banner has its matching semantic foreground; its secondary body uses that same foreground. Primary-filled cards use on-primary for every label. Do not put muted text, white small text on apricot, or status colors on arbitrary surfaces. Decorative diagrams use no essential color-only encoding. Disabled controls keep readable text and add an unavailable label and dashed outline rather than fading opacity.

## Contrast evidence

Ratios are computed from linearized sRGB relative luminance with `(Llighter + .05)/(Ldarker + .05)` using [WCAG's minimum contrast guidance](https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum.html). Every text pair below passes 4.5:1, including captions and disabled labels. `line` and `focus` rows are non-text UI only and pass 3:1. Large text is permitted at 3:1 but uses the stronger text pairs here. Values are rounded for display; checks compare unrounded values.

| Mode | Foreground / background | Hex pair | Contrast | Minimum |
| --- | --- | --- | --- | --- |
| light | text / canvas | #242038 / #F7F5F0 | 14.40:1 | 4.5:1 |
| light | muted / canvas | #5F596F / #F7F5F0 | 6.13:1 | 4.5:1 |
| light | primary / canvas | #5942B5 / #F7F5F0 | 6.71:1 | 4.5:1 |
| light | line / canvas | #80768F / #F7F5F0 | 3.93:1 | 3:1 |
| light | focus / canvas | #5942B5 / #F7F5F0 | 6.71:1 | 3:1 |
| light | text / surface | #242038 / #FFFFFF | 15.69:1 | 4.5:1 |
| light | muted / surface | #5F596F / #FFFFFF | 6.68:1 | 4.5:1 |
| light | primary / surface | #5942B5 / #FFFFFF | 7.31:1 | 4.5:1 |
| light | line / surface | #80768F / #FFFFFF | 4.28:1 | 3:1 |
| light | focus / surface | #5942B5 / #FFFFFF | 7.31:1 | 3:1 |
| light | text / raised | #242038 / #EDE8FA | 13.09:1 | 4.5:1 |
| light | muted / raised | #5F596F / #EDE8FA | 5.57:1 | 4.5:1 |
| light | primary / raised | #5942B5 / #EDE8FA | 6.10:1 | 4.5:1 |
| light | line / raised | #80768F / #EDE8FA | 3.57:1 | 3:1 |
| light | focus / raised | #5942B5 / #EDE8FA | 6.10:1 | 3:1 |
| light | on-primary / primary | #FFFFFF / #5942B5 | 7.31:1 | 4.5:1 |
| light | on-primary / primary-hover | #FFFFFF / #483398 | 9.53:1 | 4.5:1 |
| light | on-accent / accent | #38250C / #F4BE69 | 8.63:1 | 4.5:1 |
| light | success / success-bg | #216446 / #E5F3EB | 6.17:1 | 4.5:1 |
| light | danger / danger-bg | #A52D45 / #FBECEF | 5.99:1 | 4.5:1 |
| light | warning / warning-bg | #774617 / #FFF0D6 | 6.99:1 | 4.5:1 |
| dark | text / canvas | #F6F1FF / #171420 | 16.37:1 | 4.5:1 |
| dark | muted / canvas | #C4BBD4 / #171420 | 9.86:1 | 4.5:1 |
| dark | primary / canvas | #BCA6FF / #171420 | 8.67:1 | 4.5:1 |
| dark | line / canvas | #9C90AF / #171420 | 6.07:1 | 3:1 |
| dark | focus / canvas | #D9CBFF / #171420 | 12.04:1 | 3:1 |
| dark | text / surface | #F6F1FF / #231F30 | 14.46:1 | 4.5:1 |
| dark | muted / surface | #C4BBD4 / #231F30 | 8.71:1 | 4.5:1 |
| dark | primary / surface | #BCA6FF / #231F30 | 7.66:1 | 4.5:1 |
| dark | line / surface | #9C90AF / #231F30 | 5.36:1 | 3:1 |
| dark | focus / surface | #D9CBFF / #231F30 | 10.64:1 | 3:1 |
| dark | text / raised | #F6F1FF / #302A42 | 12.34:1 | 4.5:1 |
| dark | muted / raised | #C4BBD4 / #302A42 | 7.43:1 | 4.5:1 |
| dark | primary / raised | #BCA6FF / #302A42 | 6.54:1 | 4.5:1 |
| dark | line / raised | #9C90AF / #302A42 | 4.57:1 | 3:1 |
| dark | focus / raised | #D9CBFF / #302A42 | 9.08:1 | 3:1 |
| dark | on-primary / primary | #22163E / #BCA6FF | 8.01:1 | 4.5:1 |
| dark | on-primary / primary-hover | #22163E / #CDBEFF | 9.91:1 | 4.5:1 |
| dark | on-accent / accent | #38250C / #F4BE69 | 8.63:1 | 4.5:1 |
| dark | success / success-bg | #A1DDBB / #19392D | 8.16:1 | 4.5:1 |
| dark | danger / danger-bg | #FFABBC / #482332 | 7.56:1 | 4.5:1 |
| dark | warning / warning-bg | #F1CD99 / #3D3020 | 8.48:1 | 4.5:1 |

Recompute with `mise exec node -- node docs/design/prototype/check.mjs --tokens`. All pairs are enumerated; no opacity, gradient or image background may be introduced under text without a new measurement. Captions sit on an opaque surface. Focus uses a surface gap so it is measurable even on a filled button. Chart marks and selected borders also need shape/labels.

## Typography and layout

System font stack: `ui-sans-serif, system-ui, sans-serif`; no font download. Keep Unicode fallbacks for non-Latin scripts. Sizes are rem equivalents of the following pixels at a 16px root; use no fixed-height text containers.

| Token | Size / line height | Weight | Use |
| --- | --- | --- | --- |
| small | 12 / 18 | 650 | Eyebrow, metadata only |
| caption | 14 / 21 | 400 or 650 | Captions, help, nav |
| body | 16 / 24 | 400 | Reading, forms, choices |
| title | 20 / 23 | 650 | Card and section heading |
| heading | 28 / 32.2 | 800 | Screen title on mobile |
| display | 40 / 46 | 800 | Desktop screen title |

Body leading 1.5; heading leading 1.15. Caption text can be enlarged independently in the player. Tabular numbers for XP, credits and time. No all-caps sentences; use small sentence-case eyebrows. Limit reading lines to roughly 60–70 characters.

Spacing uses a 4px base: `space.0/1/2/3/4/5/6/8/10/12/16` = 0/4/8/12/16/20/24/32/40/48/64px. Cards use 24px padding desktop, 16px mobile; section gap 32px; related control gap 8 or 12px. Radius sm 8, md 16, lg 24, pill 999px. Elevation flat 0; tactile press 0 4px 0 primary-edge. Use a visible border for dialogs, avoiding ambiguous blur shadows.

Size tokens: target 44px, path node 64px, sidebar 224px, support rail 280px, reading column 680px, full shell 1280px, focused shell 1000px, dialog/toast maximum 600px, slide frame 340px (300px mobile), minimum timeline/note area 120px. Typography tracking: wide 1.2px, tight −0.8px. Modal backdrop opacity is 0.9. Below 48rem: one column, 16px gutters and a bottom tab bar with safe-area padding. At 48rem: sidebar + flexible main, 32px gutters. At 75rem: optional support rail. A 30rem optional breakpoint adjusts card columns, not navigation. Breakpoints are layout thresholds, not device detection. Fixed navigation must reserve its full rendered height, including wrapped translations and safe area. Focused player/test hide peripheral navigation but keep Exit and return context.

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
| Path node | 64px circle/rounded tile plus ordered lesson label, linear DOM order | Completed check+“Completed”; current apricot+“Ready”; generating labelled spinner; locked padlock+prerequisite; failure Retry; offline-cached badge. No click target for unavailable lessons |
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
