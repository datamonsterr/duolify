# Information architecture

The active course is the learning context, not the whole account. Learn resumes its path; Courses switches or creates courses. Tutor chat, notes and bookmarks always show their course context. Changing courses does not overwrite another course's progress.

## Navigation model

At ≥48rem use a 224px sidebar: Learn, Courses, Friends, Leagues, Quick notes, Bookmarks; Credits and Profile/Settings follow below. At ≥75rem a 280px support rail holds streak, daily goal and credit summary. The rail is supplementary; every item also has a route. Keep the current destination marked by text and shape, with `aria-current=page`.

Below 48rem use five labelled bottom tabs: Learn, Courses, Friends, Notes, More. More opens a route containing Leagues, Bookmarks, Credits, Profile and Settings as ordinary links. Credits also has a labelled balance shortcut in the header. Notes maps to the Quick notes destination. Reserve tab height plus safe-area padding, use no horizontal tab scrolling and preserve each tab's scroll position. The More tab is current for its descendant destinations.

Player and lesson test use a focused shell: Exit, course/lesson breadcrumb, progress, main content, playback or test action. Exit pauses playback, preserves the current slide/attempt and returns to the path. Completion is a distinct result screen. Tutor/quick-note sheets overlay the player, pause it and restore focus without losing slide position; larger course-level chat remains a route.

## Route map

These are design destinations, not newly registered routes. Existing TanStack Router code uses code-based routes and `$param` syntax. Preserve those paths and extend the tree; see the [official path-parameter guide](https://tanstack.com/router/latest/docs/guide/path-params).

| Path template | Screen / navigation | Status and URL state |
| --- | --- | --- |
| `/` | Learn | Existing; resolve last active enrolled course, else catalog empty state |
| `/courses` | Course catalog | Existing; validated `filter=enrolled|discover`, `query` local unless explicitly shared; no sensitive topic in URL |
| `/courses/new` | Creation wizard | Proposed; `step=topic|preview|outline|confirm`; opaque preview ID in memory, not learner topic |
| `/course/$courseId` | Course learn path | Existing; unit expansion is local, scroll restored |
| `/course/$courseId/outline` | Enrolled course outline | Proposed; read-only units/goals and per-course level link |
| `/lesson/$lessonId` | Lesson player | Existing; validated `slide=<opaque-id>` deep link; default first or resume slide |
| `/lesson/$lessonId/test` | Lesson test | Proposed; attempt identity is server-owned, never correct answer in URL |
| `/lesson/$lessonId/complete` | Finish | Proposed; guard on verified pass, refresh re-fetches result |
| `/onboarding` | Optional learner context | Proposed; authenticated, resume locally after auth |
| `/profile` | Learner profile | Existing; personal view, not public friend profile |
| `/profile/course/$courseId` | Per-course level | Proposed; override and reset-to-global controls |
| `/streak` | Streak calendar and freezes | Proposed; local day and last rollover reconciliation |
| `/friends` | Friends, requests, friend streaks | Proposed; `tab=friends|requests` |
| `/friends/$friendId` | Friend detail | Proposed; allowed public fields only |
| `/invite/$inviteId` | Friend invitation | Proposed; validate signed opaque invite, auth then accept |
| `/leagues` | Current weekly league | Proposed; validated `week=<week-id>` |
| `/credits` | Credits wallet and daily meter | Existing; transaction pagination by opaque cursor |
| `/credits/shop` | Paywall/shop | Proposed; optional enum `intent=chat|note|outline|lesson` and safe internal return target |
| `/credits/return` | Checkout reconciliation | Proposed; external browser return, opaque session reference; never trust query success flags |
| `/course/$courseId/chat` | Tutor chat | Proposed; optional validated lesson context; opaque thread ID |
| `/notes` | Quick notes | Proposed; course/tag/review filters; private search stored locally |
| `/notes/$noteId` | Quick note detail | Proposed; source link, original text and enrichment separated |
| `/bookmarks` | Bookmarks | Proposed; course filter, unavailable targets removable |
| `/settings` | Settings | Proposed; appearance, motion, audio, captions, locale, time zone, analytics, data/account |
| `/more` | Mobile More | Proposed; regular navigation hub, reachable on desktop too |
| unmatched / unavailable ID | Not found / access recovery | Proposed; safe return to Courses, no object existence leak |

Route loaders validate identifiers, search enums and membership before fetching restricted content. Use pending/error/not-found route boundaries and TanStack Query's canonical server state. Preserve existing domain IDs; do not couple UI labels to route IDs. Use native links for destinations and buttons for actions; normal browser Back works through wizard steps and deep-linked sheets. Ask before leaving only when a draft cannot be safely retained.

## Deep links and restoration

Use the same canonical app path for web links and future Tauri app/universal-link dispatch; scheme/host registration is an implementation decision, not configured here. Examples: `/course/astronomy`, `/lesson/sky-turns?slide=earth-rotation`, `/notes/n_42`, `/invite/i_42`. No tokens, text answers, note bodies or chat messages in URLs. Authorization is rechecked after sign-in. Reject external `returnTo` targets and checkout URLs supplied by arbitrary links.

Opening a bookmark resolves its lesson and slide ID, fetching if needed; offline cached content plays, otherwise show Download when online. Missing content shows “This saved slide is unavailable” with Remove bookmark. A locked lesson link explains the prerequisite and links to the current lesson. A test link resumes a server attempt if eligible, otherwise returns to its lesson. A finish link cannot grant completion.

After verified checkout, refresh the authoritative wallet, show pending until the payment webhook has posted credits and explicitly ask whether to resume the original action. Never auto-send a drafted chat or auto-charge a generation job on return. Cancel keeps the draft. On app restore, reconcile server progress before displaying XP/streak changes; offline pending progress is visibly “Waiting to sync.” Scroll and focus return to the originating course node where possible.

## Source drift to resolve before integration

Older lesson-test passages allow an 80% pass and identical reordered retries. Older credits/social/gamification passages reward credits or sell freezes. This design follows the owner, glossary and ADR 0007: all concepts resolved correctly, later paraphrased variants, effort-earned auto-applied freezes, no earned credits. Shared friend-streak freeze consumption and E12 pricing remain open. The app's current “Finish lesson” bypasses a test; implementation must replace it with “Take lesson test.”
