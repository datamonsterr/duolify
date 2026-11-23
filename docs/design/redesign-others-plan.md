# Redesign plan: remaining client screens

Status: proposed (2026-11-23). Owner reviews each PR; nothing merges without review.

Scope is every client screen **not** owned by the conversational-flows-and-mascot work: guided chat, accounts, design primitives (`features/design`), characters, credits, mascot, the immersive lesson overlay, sign-in/sign-up/onboarding/brief, and the shell frame (`AppShell.tsx`, touched by #372) are out of scope. `components/toast`, `features/voice` and `QuickNotePanel.tsx` were just polished by #380/#383, so they are listed but left alone.

Sources: [design-system.md](design-system.md), [screens.md](screens.md), [flows.md](flows.md), [information-architecture.md](information-architecture.md), [brand.md](brand.md), [canvas](canvas/), and the in-flight phone rules on #372 (one primary per view, at most two secondary, dialogs as bottom sheets below 48rem, icons from `Icon.tsx` with visible labels).

## Rules for every PR

1. Colours, type, radius and spacing come from the CSS variables in `styles.css`/`theme.css`. No hex literals, no inline style objects for theme values, no `px` font sizes, no undefined tokens with hex fallbacks.
2. Actions use `features/design/Button.tsx` (pending → `aria-busy`, dashed unavailable state). At most one `primary` per view.
3. Targets at least 44 by 44; text grows to 200% without clipping (no fixed heights on text rows).
4. Disabled keeps readable text and a visible reason; never opacity.
5. Loading regions set `aria-busy`; empty, error and offline states have text.
6. Borders that mark a control use `--line` (3:1); decorative dividers may use the light divider.
7. No copy changes and no new localisation keys (the catalogue lives in `features/design/localization`, which is off limits). Behaviour, analytics and routes stay the same.
8. TDD: a failing render, style or e2e assertion lands with each change. Full client unit and e2e suites pass before the PR opens.

## Gap inventory and order (by user impact)

| # | Area | Main gaps | Impact | Size |
|---|---|---|---|---|
| 1 | Lesson test (`features/lessontest`) | 66 hex literals in inline styles, light-only (wrong in dark mode); `#D3D3D3` choice borders ~1.5:1 on white; `px` type; opacity-0 radio overlay; Check disabled without reason; no `aria-busy`; 11 raw buttons | High: every lesson ends here | L |
| 2 | Notes and bookmarks (`NotesPage`, `BookmarksPage`, `NoteAnchorLine`) | ~110 inline styles and ~95 hex literals; off-token colours (`#5E5E5E`, `#1667AB`); `#E0E0E0` control borders; `px` type; JS `isMobile` instead of CSS breakpoints; raw buttons; no `aria-busy` | High | L |
| 3 | Tutor chat (`features/chat`) | Legacy `.btn` buttons; opacity disabled on Send and icon buttons; fixed 3.2rem send height; blur shadow instead of border; icon-only controls without visible labels | High | M |
| 4 | Friends (`features/social`) | 36 raw buttons, several primaries per view; white on `--danger` fill (not in the contrast table, fails in dark mode); hex fallbacks; `rgb(0 0 0 / .45)` scrim; fixed heights | High | M |
| 5 | Sharing and marketplace (`features/sharing`) | 82 raw buttons, 0 `Button`; `#fff` slide background; hex fallbacks; 640px breakpoint; undefined `--size-touch`; four private dialogs with differing backdrops | High | M |
| 6 | Streak, league, daily goal, analytics (`features/gamification`) | 13 raw buttons; opacity disabled; bare loading text; focus offsets 2px vs 3px | Medium | M |
| 7 | Narration and steering | Undefined `--color-text-muted`/`--color-primary-soft`/`--radius-card` with light-only fallbacks; focus ring in `--primary`; 767px breakpoint; raw `.duo-button` | Medium | S |
| 8 | Pricing, courses, route boundary (rail cards handed off, see owner feedback) | Too many quiet actions per step; two primaries in CourseOutline/CourseCreate; `.pricing button` global override; raw Retry button | Low | S |
| 9 | Lesson player (`LessonPlayer.tsx`, non-immersive) | Inline hex toast with 0-padding Undo; 7 equal-weight controls; duplicated control sets; span progress | High, but deferred: shares the file with the immersive overlay owned by the master session | L |

Rows 1–6 ship as one PR each. Rows 7–8 ship if time allows. Row 9 waits for the overlay work to land.

## Shared-primitive requests (for the master session, not changed here)

- `Button.tsx`: add a `danger` variant (filled `--danger-bg` with `--danger` text, or outline), an optional leading icon, and a per-action pending label instead of the fixed "Preparing…".
- `styles.css`: retire the legacy `.btn`/`.btn.sec`/`.btn.vio` family (its `:disabled` uses `opacity: .6`) and the `.controls button` restyle; one button system.
- Global focus rule should also cover `select`, `textarea`, `summary`, `[role=button]`, `[role=radio]`, using `--size-focus-width`/`--size-focus-offset`.
- Define or remove `--size-touch`, `--radius-card`, `--color-text-muted`, `--color-primary-soft`; pick one naming scheme (`--font-size-*` vs `--text-*`, `--line` vs `--color-line`).
- A shared sheet/dialog primitive (the #372 `phone-sheet.css`) so feature dialogs stop hand-rolling Escape, focus and backdrop; one breakpoint (48rem) and `--opacity-overlay` for the scrim.
- `Badge.tsx`: tone icon slot and `offline`/`unavailable` roles.
- A shared feedback banner (tone icon + title + body) to replace per-feature `role=alert` divs.

## Owner feedback for the master session (2026-11-23)

Raised by the owner on screenshots of the current build. All of it sits in master-owned paths (and partly in #372), so it is handed off rather than changed here.

| Feedback | Where it lives |
|---|---|
| The `<details>` "Settings" disclosure at the top of the main column (language, appearance, reduce motion, reset) is unfriendly. Move language and a theme toggle to the top-right of the app bar, side by side; move the rest into the Settings page. | `features/design/localization/PreferenceControls.tsx`, rendered by `features/shell/AppShell.tsx` |
| Checkboxes are oversized dark-grey squares and misaligned with their labels ("Captio/ns", "Analyti/cs", "Remind/ers" wrap mid-word). Make them round, `--primary` fill when checked with an `--on-primary` tick, keep the 44px target through the label hit area, and align label and control in one responsive row or grid. | global checkbox style (`styles.css`), `features/accounts/settings.css` |
| Select boxes have no padding before the down arrow. | global `select` style in `styles.css` |
| Settings sub-sections are links stacked at the bottom; Profile already has a sidebar button. Show the sections as a horizontal tab row at the top, replacing the redundant "Settings" heading. | `features/accounts/Settings.tsx` (#372 is reworking it as a grouped list) |
| Dark theme still has contrast mismatches. | Quick notes is fixed by the notes PR of this plan; the rest is token and primitive work |
| Courses view: each row holds one or two left-aligned buttons ("Create a course", "Refresh courses", tabs, streak and credits chips), so the learner scrolls far before seeing a course. Group the actions into one toolbar row and lead with the course list. | `features/design/Courses.tsx`, `courses.css` (#372 touches it) |
| Credits page: buttons are full width and wrap to the next line. | `features/credits/*` |
| The right rail is unnecessary. Fold streak, XP and credits into one compact block in the left sidebar, and remove the left-bottom footnote so the main panel gets the width. | layout and footnote in `features/shell/AppShell.tsx`; card content in `features/shell/RailCards.tsx` (in this plan's scope, but only worth changing together with the AppShell placement, so it is handed off with it) |
