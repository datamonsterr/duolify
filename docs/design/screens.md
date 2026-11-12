# Screen and state catalog

Each numbered surface specifies purpose, layout, components, copy, analytics and accessibility. Every surface is designed desktop first and then mobile, with the same functions on both: desktop side panels become mobile sheets, list + detail becomes two screens, the sidebar becomes bottom tabs plus More. Visual reference for every screen and state: the [Duolify App Design canvas](https://claude.ai/artifact/FggicArBXWkRVSfQnk7Ukn). Wireframes show content order inside the responsive shell from [information-architecture.md](information-architecture.md). On mobile, rails become inline summaries and secondary panes become sheets. The common recovery matrix at the end applies to **every** surface, including wizard steps, not just primary routes. All amounts below are explanatory placeholders; production reads E12 config and the authoritative ledger.

Analytics below distinguish proposed client UI events (`ui_*`, catalogued in the design-system delta) from existing domain outcome events (catalogued in `analytics/spec.md`). Use `screen_id`, common platform/app_version/source properties and known course/lesson IDs. Outcome ownership stays with its implementing API/client contract. Never send text, answers or profile values. The static prototype emits no events.

## 1. Welcome and sign-in

Purpose: explain the learning loop and enter an account without requiring a purchase.

```text
[duolify]
Make room for one new idea.
[Topic → short lessons → lasting understanding]
[Continue with account]  [Sign in]
Privacy · Terms · Analytics preference
```

Components: brand, original geometric diagram, primary auth action, legal links, consent control. Copy: “Learn a topic a little at a time.” States: new/returning learner, opening auth, auth cancelled, expired link/session, offline. Recover with Retry sign-in; retain safe intended destination, never credentials. Analytics: `ui_screen_viewed(screen_id=welcome)`, `ui_control_activated(control_id=sign_in)`. Accessibility: auth has a labelled external-flow notice, focus returns after cancel, status does not depend on illustration.

## 2. Chat onboarding and learner profile

Purpose: collect optional personalization through a friendly guided chat, with informed control. Revised 2026-11-09: the earlier single form is now the fallback and the later profile editor.

```text
[Back] Let's set things up  [3 of 5]        Prefer a form? · Skip setup
Steps: Goal ✓ · What you know ✓ · Daily time ● · Reminders · Time zone
Duolify: Hi! I'll ask a few quick questions… What would you like to get better at?
You: Something about space I guess
Duolify: Space is a great pick! For fun, school or work?      ← follow-up only when vague
[5 min] [10 min] [15 min] [20+ min] [Skip]                   ← quick replies
[Type your own answer…]                              [Send]
Desktop right panel / mobile sheet: What I've understood (each line editable)
→ Confirm summary: [Looks good — plan my first course] [Change something]
```

Components: guided chat (C14) with author-labelled bubbles, quick-reply buttons with pressed state, typing indicator, step chips, free-text composer; live summary card (C15). Questions: goal, what you already know (level), daily time (daily XP goal), reminder time, time zone (detected, confirmable). Copy: “You can skip any of them.” “AI uses your answers to tailor examples.” States: first question, vague answer → one follow-up, typing, skipped item, summary updating, confirm summary, edit a line, chat unavailable → answers carried into the form fallback, saving/saved/error with draft kept. The setup chat is platform-paid (proposed; confirm in E12). Profile editing later (Settings › Profile) uses the form plus “Update by chat”, per-course level overrides, export and clear; clearing sends empty fields and keeps unrelated preferences. Analytics: `learner_profile_updated(fields_changed names only)`, `onboarding_completed`, proposed `ui_control_activated(control_id=quick_reply|skip|form_fallback)`; never send answer text. Accessibility: the transcript is a log region announcing new assistant messages once, quick replies are real buttons, every step works by typing alone, focus stays in the composer, the form fallback is always one link away.

## 3. Course catalog and course switcher

Purpose: find enrolled tracks, switch context or begin course creation.

```text
Courses                              [+ Create a course]
[My courses | Discover]  [Search courses]
[Astronomy · Unit 1 · Continue] [Plant care · Start]
```

Components: searchable course cards, enrolled/discovery tabs, current-course marker, empty CTA. Copy: “What would you like to keep learning?” States: no enrollment (“Start with a topic”), no results (“Try another search”), loading cards, stale offline list, unavailable/deleted course, request error with Retry. Analytics: `ui_navigation_selected(destination=course)`, `ui_control_activated(control_id=create_course)`. Accessibility: one linked title per card, no nested interactive card, search label and result count announced politely; long titles wrap.

## 4–7. Create course (revised 2026-11-09)

The flow is now **Brief → Preview → Outline → Create**: a clarifying chat fills a course brief (starting from onboarding answers: topics to include and skip, depth, maths level, viewing conditions, lesson length, language); the learner confirms the brief before the paid preview; after the preview they shape the sample outline — reorder units and lessons by drag handle or move-up/down buttons, rename, remove, add, undo, or ask the outline chat for changes, which come back as a diff (added, removed, moved, retitled) to Apply or Discard; then they confirm the outline job. Desktop shows the brief and the outline chat as docked side panels; mobile shows the same content as a sheet and as step screens with identical actions. Proposed pricing to confirm in E12: the clarifying chat is free; each outline chat change costs about 5 credits and may use daily free credits; manual edits are free. The confirmed outline job uses the edited outline. The sections below remain the contract for the paid preview and outline job; where they mention a topic form or a read-only outline review, the chat brief and the outline editor replace them.

## 4. Create course — topic

Purpose: define a topic and level before requesting a paid preview.

```text
Create a course                 Step 1 of 4
What would you like to learn? [topic input]
Starting level [select]  Goal [optional]
Preview estimate: {credits}; balance {balance}
[Cancel]                           [Preview course]
```

Components: stepper, topic field, optional level override, estimate/reservation explainer. Copy: “Preview examples before you create a course. A preview uses credits.” States: empty, too long, estimate loading/stale, submitting, preview failure, insufficient credits; retain text on retry/shop cancel. No engine call if balance is insufficient. Analytics: `course_preview_requested(topic_length,has_profile)`, `ui_control_activated(control_id=preview_course)`, `credits_insufficient` API-owned. Accessibility: textual step count and field error, estimate changes announced without moving focus, no automatic submit.

## 5. Create course — preview examples

Purpose: show sample slides and a sample outline before course commitment.

```text
Course preview                  Step 2 of 4
Sample, not your finished course
[Example slide] [Play] [Captions] [Next example]
Sample outline: Unit 1 … Unit 2 …
[Edit topic]                       [Review outline]
```

Components: compact player, sample badge, outline summary, regenerate action with a fresh cost estimate. Copy: “These examples show the approach. No course has been created.” States: ready, playback unavailable with transcript, partial examples/error, empty/invalid response rejected, new preview loading, cancel/discard; do not persist course or enrollment on preview. Analytics: `course_preview_viewed`, `ui_control_activated(control_id=review_outline)`, `course_generation_failed(stage=preview)` API-owned. Accessibility: examples have equivalent explanations, no autoplay, keyboard stepping and descriptive iframe title.

## 6. Create course — outline review

Purpose: inspect the proposed unit order, lesson titles and goals, without generating lesson content.

```text
Review the sample outline       Step 3 of 4
Unit 1: Finding your bearings [expand]
  Lesson 1 · Recognize a pattern
  Goal: explain why the sky changes
[Back to preview]                    [Review cost]
```

Components: accessible disclosure list, level context, sample-outline badge. Copy: “The confirmed course outline may differ. Lesson content is prepared one lesson at a time.” States: collapsed/expanded, very long outline, no valid outline with Retry, topic changed with re-preview. No unsupported drag-to-edit promise; editing topic returns to step 1. Analytics: `ui_control_activated(control_id=outline_expand|review_cost)`. Accessibility: disclosure buttons expose expanded state; heading hierarchy reflects units then lessons, no horizontal tree grid.

## 7. Create course — confirm cost and generate outline

Purpose: consent to the outline-only job with a clear price boundary.

```text
Create your course outline      Step 4 of 4
Reserve up to {estimate} credits
Daily free {daily} → purchased {paid}; held {reserved}
Narration is included in later lesson-generation estimates.
Unused reserved credits return after settlement.
[Back]                      [Create outline · up to {n} credits]
```

Components: cost breakdown, balance, limit explanation, idempotent submit. Copy: “This creates the outline and enrolls you. Lessons are prepared separately as you reach them.” States: estimate pending/expired, low balance, insufficient → shop, accepted job (“Preparing your outline…”), job reconnect, terminal failure with released reservation, success → path first lesson ready to generate. No invented progress percentages. Analytics: `ui_control_activated(control_id=confirm_outline)`, then API `course_outline_generated`, `course_enrolled`, `credits_reserved|settled|released`. Accessibility: reserve/cost labels are readable text, pending action cannot double-submit, job status is a polite live region.

## 8. Learn path

Purpose: resume the current course, understand progress and identify the next eligible lesson.

```text
[Course switcher]          [7-day streak] [Credits]
A little closer to the cosmos.
[Unit 1 · Finding your bearings · 1 of 4 complete]
  [✓] A map of the night         Completed · Review
    [▶] Why the sky changes      Ready · [Start lesson]
  [lock] Following a star        Pass previous test first
    [lock] Reading the seasons
```

Components: ordered path, unit card, complete/current/locked nodes, daily goal, earned-freeze summary, optional rail. Copy: “One small lesson. A wider view.” States: no courses, first lesson ready-to-generate with estimate, generating SSE/reconnect, ready cached, current in progress, next lesson blocked on test, next job insufficient credits, generation failed, entire course complete, offline with cached badges. A completed slide deck alone never marks a lesson complete. Analytics: `ui_screen_viewed(screen_id=learn_path)`, `lesson_generation_requested` from implementing workflow, `ui_navigation_selected`. Accessibility: linear reading order despite path offsets, text prerequisites, completion check labels, no decorative locked controls in Tab order.

## 9. Lesson player

Purpose: learn one idea per Motion HTML slide, with synchronized narration and learner-controlled pace.

```text
[Exit]       Why the sky changes        Slide 1 of 5
[slide progress]
┌ Motion HTML iframe · sandboxed ─────────────────┐
│ Original diagram with equivalent explanation   │
└────────────────────────────────────────────────┘
[Caption: Earth turns. Your view changes.]
[Play/Pause] [Replay] [timeline] [1×] [Mute]
[Captions] [Transcript] [Bookmark] [Ask tutor] [Add note]
[Previous]                            [Next slide / Take lesson test]
```

Components: sandboxed iframe (`allow-scripts`, no same-origin), strict CSP, parent-owned controls, readable caption/transcript, context sheets, preload. Copy: “Narration is ready. Press Play when you are.” States: idle, playing, paused, ended, buffering, audio unavailable (“Read the transcript and continue”), slide failure with Retry/Exit, next preload slow, cached offline, missing asset, bookmark saving/queued, note draft. Previous/next reset the aligned slide clock; speed and seek affect both media and animation. Muting leaves captions. Final slide leads to the lesson test, not completion. Exit preserves position. Analytics: `lesson_started`, `slide_paused`, `slide_replayed`, `slide_viewed` **on ended**, `lesson_player_error`, `bookmark_added`, `ui_sheet_opened(sheet_id=quick_note)`, `quick_note_created(source=slide|lesson|chat, length)`. Accessibility: keyboard controls, labelled range with time value, captions resizable, full transcript always available, pause on hidden page/sheet; reduced motion retains equivalent explanation. Narration never autoplays.

## 10. Lesson test — question

Purpose: verify every taught topic without time pressure or a lives system.

```text
[Exit to lesson]                      Lesson test
Resolved topics: 0 of 2              Retry variants queued: 0
Why do stars appear to move at night?
( ) Earth turns.    ( ) Earth stays still.
( ) Stars switch on and off.
                                    [Check answer]
```

Components: fieldset/radios, topic-progress label, retry queue badge, disabled-until-selected Check. Launch type is single-answer multiple choice. Text+diagram choices include alt text; multiple select, ordering, free text and drag exercises are future capability changes, not silently introduced types. States: no selection, selected, submitting, server grade error retaining selection, restored attempt, offline answers waiting for verification. Production never delivers the correct answer before submission. Analytics: `lesson_test_started`, `lesson_test_submitted`, `ui_control_activated(control_id=check_answer)` without answer value. Accessibility: arrow-key radio navigation, legend is question, no auto-submit on select, no timer, question change moves focus to heading after Continue.

## 11. Lesson test — correct, correction and retry variant

Purpose: teach through an error and verify the same idea later with new wording.

```text
[Choices locked after grade]
Correction: Earth rotates, changing which direction we face.
A retry variant is queued for later.          [Continue test]
… different topic first …
Retry variant · Same idea, fresh wording
What makes constellations drift across your view?
( ) Because our planet rotates. … paraphrased distractors …
```

Components: hearts-free labelled banner, explanation, queue count, resolved-topic progress, explicit Continue. Correct: “That's it. Earth turns.” Wrong: correction is shown immediately; a paraphrased prompt **and** choices join the queue after other original topics. A missed retry queues another fresh variant; no limit on learning attempts and no false completion. If nothing else remains, show the correction and explicit Continue before its next variant. A repeated topic is not counted twice. Finish becomes available only after every original topic has a correct resolution and the retry queue is empty. States: original correct/incorrect, pending variant generation, variant ready/wrong/correct, variant service failure with Retry/Return to lesson preserving queue. No credit charge for simply selecting an answer; any unresolved variant-generation pricing belongs to E4/E12 and must not silently block a promised retry. Analytics: proposed `ui_test_retry_presented(queue_size)`, existing `lesson_test_passed` only after server verification; wrong answers do not masquerade as whole-test failure events. Accessibility: banner is a concise live status, correction persists until Continue, icons accompany colors, no shaking or sound-only feedback.

## 12. Lesson finish

Purpose: acknowledge verified understanding, show earned effort rewards and hand off to next generation.

```text
[check] One idea clearer.
All topics resolved · {xp} XP · Streak {days} days
[Freeze earned if milestone reached]
Next lesson: Preparing… / Ready / Needs credits / Try again
[Back to learn path]                 [Continue when ready]
```

Components: completion summary, daily-goal bar, reward banner, next job card with estimate if needed. Copy: “You came back to the tricky idea and worked it through.” States: first pass, review (no duplicate XP), offline provisional (“Waiting to sync” with no awarded XP), next generation loading/error/insufficient balance. Only confirmed pass awards XP and unlocks N+1; next-generation failure cannot undo completion. Analytics: `lesson_test_passed`, `lesson_completed`, `xp_earned`, `daily_goal_met` and `streak_extended` from canonical owners; UI screen view is separate. Accessibility: no mandatory celebration, reduced-motion static check, reward announced once and written as text.

## 13. Streak and freezes

Purpose: explain consecutive learner-local days, earned protection and rollover results.

```text
Your streak · 7 days            Time zone: Asia/Ho_Chi_Minh
[Week calendar: complete | freeze-covered | missed | today]
Streak freezes: 1 of {cap}      Earned at {milestone}
[Notice: A freeze covered yesterday. Your streak stayed at 7.]
[Return to lesson]              [Time zone settings]
```

Components: calendar with textual day states, inventory/cap, effort milestone explanation, persistent auto-apply notice. States: active, today incomplete, milestone earned, cap reached (no credit substitute), missed+freeze consumed, missed without freeze reset, longest streak, rollover reconciling, timezone changed awaiting server reconciliation. Freeze preserves rather than increments a streak; no activation button or paid-freeze offer. Analytics: `streak_freeze_earned`, `streak_freeze_used`, `streak_extended`, `streak_broken` API-owned; `ui_control_activated(control_id=freeze_notice_acknowledge)`. Accessibility: calendar has list alternative and full dates, marks have labels not just color; midnight is not a UI animation race. Notice copy includes covered local date and remaining inventory.

## 14. Friends, requests and friend streak detail

Purpose: connect by consent and track shared effort without exposing personal learning context.

```text
Friends                      [Invite by link / Username]
[Friends | Requests]
Mina · Friend streak 4 days · You: done · Mina: pending
[Nudge] [View friend streak]
Request from Kai [Accept] [Decline]      [Block / Remove]
```

Components: friend rows, request actions, labelled shared-day statuses, rate-limited nudge, confirmation dialogs. Copy: “A shared day grows when you both finish a lesson.” States: no friends, invite copied/pending/expired, accepted/declined, username not found with neutral error, duplicate request, blocked, nudge sent/limited, both complete, one pending, shared rollover pending, freeze-covered/broken. Show each person's local completion day and “Shared day checking” until the later day-end; do not falsely finalize on the viewer's midnight. Freeze policy is config-dependent and must be resolved before shipping; design never suggests buying protection. Analytics: `friend_request_sent(method)`, `friend_request_accepted`, `friend_nudge_sent`, friend streak outcomes API-owned; sheet events UI-owned. Accessibility: names use bidi isolation, avatar decorative alongside name, nudge reason stays visible. Expose only display name/avatar/streak/today status, never notes, chat, profile, topic or goals.

## 15. Leagues

Purpose: offer an optional weekly comparison of effort measured by XP.

```text
Weekly league                   Ends {local datetime}
Your rank: 8 · {weekly_xp} XP
Promotion zone ─────────────────
1 Mina 420 XP … 8 You 240 XP …
Demotion zone ──────────────────
Last week: Stayed / Promoted / Demoted
```

Components: ordered ranking, highlighted self row, textual zone boundaries, week selector. Copy: “Every completed lesson adds to your week.” States: not yet assigned (“Complete a lesson to join”), joining, active, ties with server ranking, week rollover pending, promoted/demoted/stayed, offline cached as-of time, load failure. Do not infer final rank while rollover is pending or award credits. Analytics: `ui_screen_viewed(screen_id=leagues)`, `league_promoted|league_demoted` API-owned. Accessibility: semantic ordered list/table, readable rank label, no auto-scrolling leaderboard, no motion-dependent rank transition.

## 16. Credits wallet and daily free credits

Purpose: make available, expiring and reserved balances understandable before spending.

```text
Credits                          [View credit options]
Available: {total} · Reserved: {held}
Daily free: {remaining}/{grant}   Resets {time} {zone}
Purchased/subscription allowance: {balance}
Daily free credits are used first and expire at local day end.
[Recent activity: reserve / settle / release / purchase / expiry]
```

Components: distinct balance cards, labelled daily meter, estimate examples, ledger rows. Copy: “The daily allowance is sized for about 3 quick notes and 5–8 chat messages. Usage varies; this is one shared pool.” Never imply separate guaranteed quotas or unused credits carrying forward. States: grant available/partly spent/exhausted, reserve held, settlement/refund, reset awaiting sync, zero purchased balance, expired daily grant, cached balance marked unverified. Subscription allowance expiry/rollover terms await E12; show configured terms explicitly. Analytics: `ui_screen_viewed(screen_id=credits)`, `credits_reserved|settled|released` API-owned. Accessibility: values are text, meter has label/max/value; ledger amount signs and entry names don't rely on color; use locale number formatting and dates.

## 17. Paywall/shop: credit packs and subscriptions

Purpose: choose an optional purchase with transparent terms and an easy return to saved work.

```text
Make room for your next idea.                 [Back]
Current balance {n}; action needs up to {estimate}
[Credit packs | Subscriptions]
( ) {pack credits} · {localized one-time price}
( ) Monthly · {allowance}/month · {price}/month
( ) Yearly · {allowance}/month · {total}/year
Renewal, cancellation, taxes and allowance-expiry terms
[Continue to checkout]              [Wait for daily reset]
```

Components: radio offer cards, monthly/yearly comparison, full billed amount, current balance, estimated spend context, terms links, explicit close. Copy: “Your saved lessons and notes stay available.” No preselected recurring plan, fake discount, trial countdown or “unlimited AI” promise. States: offers loading/error/unavailable locale/platform, selected pack/monthly/yearly, active subscription/manage, action estimate expired, exhausted balance. Prices and sizes are E12-owned; prototype says “Illustrative offer — pricing pending,” and its CTA is “Preview checkout.” Analytics: `ui_purchase_option_selected(offer_id,billing_period)`, `ui_checkout_requested`; no purchase event at this stage. Accessibility: full-card native radios, period in label, no hidden renewal terms, sheets are dismissible and preserve draft/focus.

## 18. Checkout return and credits exhausted

Purpose: reconcile payment truth and recover the original action safely.

```text
Checkout opened in your browser
Payment pending. Your balance has not changed.
[Check payment status] [Return to saved work]
After verified ledger update: Credits added. [Resume action]
```

Components: external-flow notice, persistent pending banner, status check, safe return. Cancel: “No credits were added. Your draft is ready.” Failed: “Payment didn't complete. Choose another option or try again.” Verified success shows ledger amount; duplicate callbacks cannot add credits twice. Zero-credit chat preserves unsent message; raw quick note saves with enrichment pending; outline/lesson generation waits without losing prior progress. Cached lesson review, bookmarks and existing notes remain accessible. Reset option names next local reset time and requires server reconciliation; do not assume payment query parameters or elapsed time mean success. Analytics: `credits_insufficient`, verified `credits_purchased`, `credits_purchase_failed` authoritative; `ui_recovery_selected(action=retry|wait|return)`. Accessibility: no trapping modal after browser return, pending status stays readable, never announce success before ledger update.

## 19. Tutor chat and player tutor sheet

Purpose: ask within the course, lesson and learner context while controlling credit spend.

```text
Tutor chat · Astronomy · Why the sky changes  [Context]
You: Why does the sky move?
Tutor: Earth's rotation…             [Save quick note]
[Streaming reply…] [Stop reply]
[Draft message] Estimated cost {class/credits} · Balance {n}
                                             [Send]
```

Components: author-labelled bubbles, context disclosure, composer, estimate, Stop, save-to-note, thread menu/delete. Copy: “Uses this course, your current lesson and your learner profile.” States: empty with starter question, estimating, reserving, waiting for first token, streaming, stopped (charge produced tokens), complete, fallback before first token, interrupted mid-stream (“Reply interrupted; only produced tokens charged”), insufficient/offline with draft, delete confirmation. Retry mid-stream requires explicit new estimate and never silently duplicates a billable send. Analytics: `tutor_message_sent`, `tutor_reply_completed|stopped|failed`; `$ai_generation` API-owned, `ui_sheet_opened` for player sheet. Accessibility: live region announces reply ready, not every token; Stop always reachable, no focus theft on stream, plain selectable text and meaningful link labels. Sheet pauses lesson playback and restores focus/position.

## 20. Add quick note

Purpose: capture an idea immediately without making saving depend on AI or credits.

```text
Quick note · Slide 2                         [Close]
[Your note textarea]
Saved text stays yours. Enrichment adds a separate summary.
Enrichment estimate {n} credits / Pending until reset
[Save quick note]
```

Components: sheet, labelled textarea, source chip, save status, enrichment cost. Copy: “Saved. Enrichment is pending.” States: draft, local save, server save, offline queue, enrichment reserved/running/ready, zero credits, failure with raw text intact. Distinguish device-saved from synced; closing can retain draft locally without claiming backup. Analytics: `quick_note_created(source,length)`, enrichment outcomes API-owned, `ui_sheet_opened`. Accessibility: initial focus textarea, persistent status linked to field, closing returns to Add note; no auto-read of private content.

## 21. Quick notes list and detail

Purpose: find, review, export and edit learner-owned text independently from AI additions.

```text
Quick notes [Search] [Course] [Lesson] [Tag] [Not reviewed]
[Note title · source · Enrichment pending · Not reviewed]
Detail: Your original note [Edit]
AI summary · tags · lesson links (separate section)
[Mark reviewed] [Export Markdown] [Delete]
```

Components: filter chips, note rows, original/enrichment sections, source links, export, review indicator. Copy: empty “Keep an idea from your next lesson”; no results “Clear filters to see more notes.” States: loading, long text, unreviewed/reviewed, raw-only, enrichment pending/failed/retry with cost, offline edited/conflict (show both versions), source unavailable, delete confirmation/undo if supported, export loading/error. Never overwrite raw text or show hallucinated lesson links; retry preserves edits. Analytics: `quick_note_created(source=slide|lesson|chat, length)`, `quick_note_reviewed|deleted`, `quick_note_enriched|enrichment_failed`; `ui_sheet_opened(sheet_id=quick_note)`, `ui_control_activated(control_id=export_notes)`. Accessibility: filter labels/result count, Markdown-like content has semantic headings, no hover-only actions, long URLs wrap and link names identify source lesson.

## 22. Bookmarks

Purpose: return to a saved lesson or slide without generating new material.

```text
Bookmarks [Course filter]
[Why the sky changes · Slide 2 · Cached] [Open] [Remove]
[Reading seasons · Unavailable]                  [Remove]
```

Components: list rows, target-type labels, offline/cache badges, remove action. Copy: empty “Save a slide to return to an idea.” States: loaded/empty, no filtered matches, queued offline add/remove, target uncached offline, deleted/moved source unavailable, fetch error. A bookmark opens the exact slide by ID after authorization; it never silently opens a different slide. Analytics: `bookmark_added|removed|opened(target_type)`; recovery event for uncached content. Accessibility: Open names the target, Remove names the saved item, unavailable rows remain readable and removable, announce save/remove once without stealing focus.

## 23. Settings and account/data controls

Purpose: control accessibility, privacy, learning defaults and account lifecycle.

```text
Settings
Appearance [System / Light / Dark]   Motion [System / Reduce]
Narration [on request]  Speed [1×]  Captions [on]  Text size
UI sounds [off]  Haptics [OS default]
Locale [ ] Time zone [ ] Daily XP goal [ ]
Analytics [toggle]  [Export data]  [Delete account] [Sign out]
```

Components: native settings controls, concise help, save feedback, export/delete confirmations. Copy: “Turning analytics off stops future usage events.” States: loading, saved, saving/error with previous value retained, platform unsupported preference with reason, system mode, account export pending/ready/expired, delete requested/scheduled (30-day removal policy), sign-out with queued offline work warning. Changing appearance preserves test state; reducing motion wins over OS animation preference. Timezone changes explain daily reset/streak impact and await canonical state. Analytics: `analytics_opt_out_changed` handled by privacy contract (no further events after opt-out), `ui_appearance_changed(mode)`, `learner_profile_updated(fields_changed)`; no preference values that expose personal text. Accessibility: radiogroups for modes, labels/help text, destructive confirmation accessible, no color-dependent selected states.

## 24. More, not-found and access boundaries

Purpose: expose secondary destinations on mobile and recover safely from invalid links.

```text
More: Leagues · Bookmarks · Credits · Profile · Settings
Unavailable page: This page isn't available.
[Back to Courses] [Return to Learn]
```

Components: standard link list, neutral error panel, auth return when applicable. States: valid link, not found, deleted object, expired invite, signed out, no access, route pending/error. Avoid exposing whether another learner's private note exists. Analytics: `ui_navigation_selected(destination)`, `ui_recovery_selected(error_code=not_available)`. Accessibility: heading receives focus on route transition, no automatic redirect loop, browser Back remains usable.

## Shared recovery and long-content contract

| State on every surface | Layout, copy and component behavior | Analytics / accessibility |
| --- | --- | --- |
| Initial loading | Stable content shell + labelled static skeleton; “Loading your courses…”; never shimmer in reduced motion | `aria-busy`, single polite status; screen view once when route visible, not on every render |
| Long background job | Persistent job card, named stage, safe Leave action; “Preparing your lesson. You can come back.” | No invented %; reconnect to existing job ID rather than re-create/charge; UI recovery enum |
| Empty / first use | Heading, short explanation, one relevant CTA; no fake content | Describe outcome, focus CTA only on user navigation; screen-specific examples above |
| No search results | Keep query and filters, show Clear filters, no purchase upsell | Result count status; private query excluded from analytics |
| Offline cached | Banner “Offline. Saved lessons are available”; as-of timestamp; notes/bookmarks queued once | No AI/payment action attempted; distinguish local progress from verified completion |
| Offline uncached | Explain missing content; Return to saved lessons; Download when online | Action reason visible, no dead spinner; `ui_recovery_selected(error_code=offline_uncached)` |
| Reconnect / sync | “Syncing saved work…” then canonical state; conflict preserves both drafts | Idempotent event replay; duplicate progress does not duplicate XP/charges; announce only result |
| Recoverable error | Inline panel names failed action; preserve input; Retry/Back; correlation ID available in details | `role=alert` for blocking failure, focus first invalid field; no raw stack/provider secrets |
| AI terminal error | “Couldn't prepare this. Reserved credits were released.” only after API confirmation; Retry with new estimate | Existing capability timeouts/fallback/release; validated generated content only; `ui_recovery_selected` |
| Session expired | Auth return prompt; preserve safe draft/route, recheck access after login | No credentials or private text in return URL; clear personal cached data on sign-out per runtime policy |
| Long/localized content | Wrapping titles/buttons, vertical growth, logical margins, 40% expansion, bidi-isolated names; no fixed caption height | 200% text, 320px reflow, RTL; media controls remain directionally correct |
| Destructive action | Named object, consequences, Cancel and explicit Delete; state/error retained | Focus trap/restore, keyboard Escape; no single-tap delete-account action |

The design creates no production data. Runtime profiles, notes, attempts, friendships and balances retain their capability's PostgreSQL backup/deletion rules; slides/audio/manifests use versioned object storage. Device drafts/cache are never the only backed-up copy after sync. No model or prompt changes occur here; feature implementations require golden-set coverage and `engine:eval` for AI behavior, including retry-variant validity, narration alignment, grounding and graceful failure.
