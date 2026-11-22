# design-system Specification

## Purpose
Define the shared design tokens, accessibility, responsive shell, sign-in, settings, tutor chat and brand identity behavior that every client surface follows.

## Requirements

### Requirement: Shared token usage
Duolify UI implementations SHALL use the semantic color, typography, spacing, radius, elevation and motion tokens defined in `docs/design/tokens.json`. Implementations MUST NOT introduce literal visual values outside token definitions, except content-driven geometry and documented platform safe-area values. `docs/design/theme.css` SHALL map the same values to Tailwind v4. The offline prototype SHALL embed matching tokens and MUST NOT wire them into the client.

#### Scenario: Component styling review
- **WHEN** a new application component is reviewed
- **THEN** its visual styles resolve to documented tokens and any geometry exception has an explanation

#### Scenario: Token artifact parity
- **WHEN** a token changes
- **THEN** JSON, the CSS snippet, contrast evidence and embedded prototype values are updated together

### Requirement: WCAG 2.2 AA accessibility
UI SHALL meet WCAG 2.2 AA. Normal text MUST have contrast of at least 4.5:1; large text and meaningful UI graphics MUST have at least 3:1. Controls MUST have accessible names, visible focus, keyboard operation and status feedback independent of color. Layouts MUST reflow at 320 CSS pixels and support 200% text resizing. Duolify's target size SHALL be at least 44 by 44 CSS pixels.

#### Scenario: Incorrect lesson answer
- **WHEN** an answer is incorrect
- **THEN** an announced correction and labelled retry queue appear without relying on color or removing a life

#### Scenario: Modal keyboard use
- **WHEN** a keyboard user opens a sheet or dialog
- **THEN** focus enters it, stays inside until closure, Escape closes it and focus returns to the trigger

### Requirement: Reduced motion and controllable narration
UI SHALL honor `prefers-reduced-motion` and an explicit learner setting. Reduced motion MUST remove nonessential spatial motion, automatic scrolling and celebratory animation while preserving the full instructional meaning. Narration MUST start only on learner action and SHALL offer pause, replay, speed, mute, captions and a readable transcript. Audio and slide time MUST stay aligned.

#### Scenario: Reduced motion lesson
- **WHEN** a learner requests reduced motion
- **THEN** a static slide and the full equivalent explanation remain available with working playback controls

### Requirement: Dark mode
UI SHALL support system, light and dark appearance using semantic token groups. Both modes MUST preserve contrast and non-color state indicators without changing learner progress.

#### Scenario: Appearance changes during a test
- **WHEN** a learner switches appearance
- **THEN** the same question, selection and retry queue remain and all colors use the selected mode's tokens

### Requirement: Responsive navigation and breakpoints
UI SHALL use mobile layout below 48rem, sidebar layout at 48rem and above, and an optional support rail at 75rem and above. Focused lesson and test routes MUST prioritize instructional space. Navigation MUST expose the same destinations at each size and MUST NOT cover focused controls or content.

#### Scenario: Narrow learn path
- **WHEN** a learner opens the path at 320 CSS pixels
- **THEN** bottom navigation, readable lesson labels and the start control fit without horizontal scrolling

### Requirement: Desktop-first parity across platforms
Every surface SHALL be designed for desktop first and then for mobile, and both MUST expose the same functions, states and recovery actions. Desktop side panels (course brief, outline chat, tutor, quick note, transcript) SHALL become mobile sheets or step screens; desktop list-and-detail views SHALL become two mobile screens; the desktop sidebar SHALL map to mobile bottom tabs plus More. A function MUST NOT exist on only one platform, except internal tooling delivered as one responsive page.

#### Scenario: Course brief on mobile
- **WHEN** a learner on a phone needs the course brief that desktop shows as a side panel
- **THEN** the same brief, with the same edit and confirm actions, opens as a sheet and as the confirm step

### Requirement: Guided conversational setup
Onboarding and course creation SHALL use a guided chat that asks one question at a time, offers quick-reply buttons and free text, asks a follow-up only for a vague answer, and lets every question be skipped. A live summary (learner profile) or course brief SHALL update as the learner answers, every line MUST be editable, and the learner MUST confirm it before anything is saved or any paid step starts. If the assistant is unavailable, collected answers MUST carry over to an equivalent form. Transcripts MUST announce new assistant messages once, without announcing each token.

#### Scenario: Vague onboarding answer
- **WHEN** the learner answers “something about space”
- **THEN** the assistant asks one clarifying follow-up with quick replies and records the topic in the live summary

#### Scenario: Preview needs consent
- **WHEN** the course brief is complete
- **THEN** no preview is generated or charged until the learner confirms the brief and its cost

### Requirement: Editable sample outline
Before the outline job is confirmed, the learner SHALL be able to reorder units and lessons by drag handle and by move-up/down buttons, rename, remove and add items, undo changes, and request changes in chat. Chat changes MUST be shown as a diff (added, removed, moved, retitled) that the learner applies or discards. Reordering MUST NOT be drag-only and position changes MUST be announced. The confirmed outline job SHALL use the edited outline.

#### Scenario: Keyboard reorder
- **WHEN** a keyboard user moves Unit 3 above Unit 2 with the move buttons
- **THEN** the order updates, “Unit 3 moved to position 2” is announced and focus stays on the moved unit

### Requirement: Localization-ready copy
UI copy SHALL use stable message identifiers, complete translatable sentences, locale-aware plural/number/date formatting and logical layout properties. Strings MUST allow at least 40% expansion and right-to-left layout. Credit costs, local-day resets and plan periods MUST be explicit. Prototype English strings SHALL be maintained as authored fixtures rather than claims of shipped localization.

#### Scenario: Long translated action
- **WHEN** a localized button label expands by 40%
- **THEN** it wraps without truncating meaning or overlapping another control

### Requirement: UI analytics naming and privacy
New interaction events SHALL follow `ui_<surface>_<action>` in lower snake case. The design-system catalog SHALL include `ui_screen_viewed` (screen_id), `ui_navigation_selected` (destination), `ui_control_activated` (surface, control_id), `ui_sheet_opened` (sheet_id), `ui_purchase_option_selected` (offer_id, billing_period), `ui_checkout_requested` (offer_id), `ui_recovery_selected` (surface, error_code, action), `ui_test_retry_presented` (queue_size), and `ui_appearance_changed` (mode). Implementations MUST use existing domain events for committed outcomes and MUST NOT duplicate authoritative API events. Properties MUST include applicable analytics common properties and MUST NOT contain learner free text, answers, note/chat/profile content or payment details. Opt-out MUST stop all events. The offline prototype MUST make no analytics requests.

#### Scenario: Purchase option selected
- **WHEN** a learner selects a credit offer
- **THEN** the client records `ui_purchase_option_selected` with an opaque offer identifier and billing period, and only a verified payment may produce `credits_purchased`

#### Scenario: Analytics opt-out
- **WHEN** analytics is disabled
- **THEN** neither UI nor domain analytics are sent for that learner

### Requirement: Design data backup and model reliability boundary
Design documents and tokens SHALL be versioned in Git and restored from repository history. The prototype MUST contain only authored fixtures and ephemeral state and MUST NOT store learner data or call an AI provider. AI implementations consuming the design MUST retain their capability's golden-set, timeout, fallback, credit-release and backup requirements; this design change MUST NOT claim those runtime guarantees are implemented.

#### Scenario: Prototype reopened
- **WHEN** the offline prototype is reopened
- **THEN** it starts from demo state without restoring any learner or payment data

#### Scenario: AI generation failure design
- **WHEN** a future feature exhausts provider retries
- **THEN** its UI offers recovery while preserving learner work and reflects reservation release from the API

### Requirement: Client green theme foundation
The client SHALL import the canonical semantic CSS mapping and bundle licensed Nunito WOFF2 weights 600, 800 and 900 without runtime font-service requests. The course list, cards, buttons and path SHALL use the revised light/dark tokens and 72px node size. Lime SHALL be used only for text-free progress and decorative geometry. Cards SHALL expose one linked title and semantic, labelled progress. Controls SHALL retain 44px targets, visible focus, pending duplicate protection and reduced-motion parity.

#### Scenario: Course recovery and reflow
- **WHEN** a learner opens Courses at desktop or mobile width
- **THEN** the same search, loading, empty, offline, unavailable and error/retry states are readable in both themes, with long content and 200% text

### Requirement: Course controls analytics boundary
Course controls SHALL emit the catalogued `ui_control_activated` event with stable control IDs through the shared analytics boundary only when an enabled authenticated UUID context is present. Events SHALL include common properties and SHALL omit learner text. Consent revocation and analytics failure SHALL never prevent navigation or recovery.

#### Scenario: Course search privacy
- **WHEN** an opted-in learner clears a course search
- **THEN** the event includes only surface `courses`, control_id `clear_search` and common properties, never the query

#### Scenario: Missing analytics context
- **WHEN** E1-06 has not supplied an enabled learner context or the learner opts out
- **THEN** course controls work without emitting an analytics event

### Requirement: Reusable guided chat and summary components
The client SHALL provide C14 author-labelled chat bubbles, one current question, pressed quick replies, free text, Skip and a form fallback link. It MUST announce each completed assistant message once per stable message ID and never streamed tokens. C15 SHALL expose editable summary lines, unanswered labels, unsaved/pending/error status and explicit confirmation. Drafts MUST survive switching between chat and form.

#### Scenario: Assistant completion
- **WHEN** a streaming assistant message completes and rerenders
- **THEN** its completed text enters the polite log once and composer focus stays unchanged

#### Scenario: Form fallback
- **WHEN** chat is unavailable and the learner selects Prefer a form
- **THEN** the same draft fields and confirmation are available with associated errors and native radio/toggle controls

### Requirement: Editable outline and proposal primitives
C16 SHALL expose unit and lesson rows with drag handles and keyboard move buttons, rename/remove/add/undo, textual position announcements and focus retention. Proposal diffs MUST label Added, Removed, Moved and Retitled and require Apply or Discard. Proposal application SHALL be undoable.

#### Scenario: Keyboard edit
- **WHEN** a unit or lesson is moved using its move control
- **THEN** the new position is announced and focus remains with that item

#### Scenario: Proposal discarded
- **WHEN** the learner discards a proposal
- **THEN** the outline stays unchanged and the action has persistent textual feedback

### Requirement: Responsive modal composition
C17 SHALL compose the same editor subtree as a desktop side panel at 48rem and a mobile sheet below 48rem. Native modal sheets MUST trap/restore focus, close on Escape and pause an optional player host on opening. Both presentations SHALL support 320px reflow, 44px targets, light/dark tokens, reduced motion and loading/empty/offline/error/long content recovery. Feedback MUST use text, not color or opacity alone.

#### Scenario: Resize with an edited draft
- **WHEN** the viewport changes between panel and sheet
- **THEN** draft values remain and all editing and confirmation actions stay available

### Requirement: Guided UI analytics catalogue
The client SHALL deliver `ui_sheet_opened` with `sheet_id` in `course_brief|setup_summary|outline_chat|fixture_sheet` and `ui_control_activated` with `surface` in `guided_chat|summary|outline|sheet` and `control_id` in `quick_reply|send|skip|form_fallback|retry|confirm|edit|move_up|move_down|drag_reorder|rename|remove|add|undo|apply|discard|open|close`. Events MUST include UUID `distinct_id`, platform, app_version and source=client through the existing consent boundary. Unknown IDs and private text MUST be excluded at runtime. No domain outcomes SHALL be emitted.

#### Scenario: Privacy-safe delivery
- **WHEN** an enabled UUID learner opens a sheet or activates a component control
- **THEN** the PostHog double receives only catalogued enums and common properties

#### Scenario: Opt out
- **WHEN** identity is absent, invalid or opted out, or PostHog capture is opted out
- **THEN** no guided UI event is delivered

### Requirement: Draft data backup and reliability boundary
Component source and authored fixtures SHALL be Git-backed and restorable from the same revision. Fixture drafts MUST live only in memory and disappear on unmount/reload; no persisted data changes require a database restore. No AI calls SHALL be added; consuming feature evaluation, timeout, fallback and credit-release gates MUST remain intact.

#### Scenario: Fixture reset
- **WHEN** the fixture is reloaded after editing
- **THEN** authored defaults return without restoring learner text from device storage

### Requirement: Shared application shells
The client SHALL expose Learn, Courses, Friends, Leagues, Quick notes, Bookmarks, Credits, Profile and Settings through a sidebar at 48rem and five labelled bottom tabs below 48rem. More SHALL link secondary destinations and remain current on their routes. A support rail MAY appear at 75rem. Route, scroll and in-progress child state SHALL survive resize. Navigation SHALL reserve its rendered height and safe areas at 320px and 200% text.

#### Scenario: Mobile More navigation
- **WHEN** a learner opens Credits from More
- **THEN** existing pricing remains available, More is current, and browser Back returns to More

### Requirement: Focused and adaptive layout contracts
Lesson and test routes SHALL hide peripheral navigation and retain exit context and progress. Shared layout hosts SHALL map desktop panels to mobile modal sheets or steps and list/detail panes to separate mobile surfaces without duplicated child state. Modal focus SHALL be contained and restored. Pending, error and unavailable boundaries SHALL expose safe recovery. Navigation SHALL focus the route heading and provide a skip link.

#### Scenario: Resize during editing
- **WHEN** a desktop panel containing a draft becomes a mobile sheet or step
- **THEN** the same draft and actions remain, and closing restores focus to its trigger

### Requirement: Shell analytics catalogue
The catalogue SHALL include `ui_screen_viewed(screen_id)` and `ui_navigation_selected(destination)`. Shell emitters SHALL accept only allowlisted destination/screen identifiers, include UUID identity, platform, app_version and source, and suppress delivery after opt-out. No URL, route parameter, learner text or private query SHALL be delivered.

#### Scenario: Private deep link
- **WHEN** an opted-in learner navigates to a private object route
- **THEN** analytics contains only its screen enum and common properties, never its ID or query

### Requirement: Disposable shell state
Shell source SHALL be restored from Git. Navigation and layout state SHALL remain disposable memory state and create no new backup or deletion dependency. No new AI behavior SHALL bypass feature evaluation or fallback gates.

#### Scenario: Fresh host
- **WHEN** a shell or layout host is destroyed and recreated
- **THEN** ephemeral draft and navigation state are reset without deleting server-owned data

### Requirement: Session recovery and device controls
Session screens SHALL follow S01 and S23 with token colors, light/dark and reduced-motion support, mobile step screens, keyboard focus, wrapping content, loading, empty and offline/error recovery states. Analytics catalogue: ui_recovery_selected(surface=sign_in,error_code=session_expired,action=sign_in). The existing PostHog wrapper MUST send only those stable properties plus verified UUID, platform, app_version and source, honor both opt-out boundaries and exclude private text, tokens and destinations.

#### Scenario: Recovery selection
- **WHEN** a known learner selects Sign in again after session expiry
- **THEN** the opted-in PostHog test double receives the catalogued common properties without private content and the email-code step receives focus

### Requirement: Google sign-in interaction analytics catalogue

The design-system analytics catalogue SHALL define the Google sign-in `ui_control_activated` event with `surface=sign_in` and `control_id=sign_in`, and the cancellation `ui_recovery_selected` event with `surface=sign_in`, `error_code=auth_cancelled`, and `action=retry`. Events MUST include a UUID identity and common properties when delivered, respect opt-out, and MUST NOT include email, authorization code, state, token, provider subject, or learner text.

#### Scenario: Google sign-in interaction

- **WHEN** a Learner starts Google sign-in
- **THEN** the client emits `ui_control_activated` with only the catalogued control and common properties

#### Scenario: OAuth cancellation

- **WHEN** a Learner cancels Google sign-in
- **THEN** the client emits `ui_recovery_selected` with the fixed cancellation code and retry action, if analytics is enabled

### Requirement: Localized appearance controls
The client SHALL provide stable message identifiers, English fallback, Arabic RTL, an expanded-copy fixture and native Intl plural, number, date and currency formatting. Route identity and editing state SHALL survive preference changes. Names SHALL be bidi-isolated. Layout SHALL reflow at 320px and 200% text.

#### Scenario: Mid-form preferences
- **WHEN** a learner changes locale, appearance or motion while editing chat, summary or outline
- **THEN** drafts, quick reply selections, focus, proposal and undo state remain intact and translated controls wrap

### Requirement: Restrictive motion resolution
System or learner reduced motion SHALL suppress spatial movement, automatic scrolling and decorative animation. Instructional content SHALL retain a static equivalent explanation.

#### Scenario: System reduction wins
- **WHEN** the system requests reduced motion and the user preference does not
- **THEN** the effective preference remains reduced, including after a system setting change

### Requirement: Private preference analytics and lifecycle
The client SHALL emit ui_appearance_changed with mode in system, light or dark and settings ui_control_activated with control_id in locale, motion or reset only. Events SHALL include common UUID properties, respect identity and transport opt-outs and exclude learner content. Preferences SHALL use a host adapter where available and otherwise remain disposable memory; reset SHALL clear them.

#### Scenario: Reset and restore
- **WHEN** the preference host is reset or recreated without a durable adapter
- **THEN** defaults return without restoring drafts or sending private data

#### Scenario: Account locale and session navigation
- **WHEN** E1 mounts the authenticated learner shell
- **THEN** the initial and reset UI language use the supported account locale with English fallback, and Settings links to the existing `/settings/sessions` route using localized copy

### Requirement: Settings screen
Settings SHALL provide appearance, motion, narration, narration speed, captions, text size, UI sounds, haptics, locale/time zone, daily XP goal, reminders and analytics consent controls. It SHALL show loading, saved and recoverable error states. Desktop sections SHALL become stacked mobile sections with the same actions. Profile SHALL retain its form editor and Update by chat entry.

#### Scenario: Save settings
- **WHEN** a learner changes settings and saves
- **THEN** the saved values remain visible and survive reload

#### Scenario: Save failure
- **WHEN** a settings update fails
- **THEN** the last saved values remain identifiable, the draft is retained, and the learner can retry

### Requirement: Settings analytics events
The catalogue SHALL include `ui_appearance_changed` with `mode` limited to system, light or dark. Events SHALL use only stable enum values and UUID/common properties.

#### Scenario: Appearance change
- **WHEN** an opted-in learner saves a changed appearance
- **THEN** one `ui_appearance_changed` event is delivered after persistence

### Requirement: Settings event privacy and retention
Settings events SHALL contain only the stable appearance enum, UUID and common properties. Preference values SHALL be backed up in Postgres with the learner profile and SHALL be removed on account deletion; PostHog SHALL remain derived.

#### Scenario: Settings restore
- **WHEN** the profile is restored from the nightly dump
- **THEN** saved settings remain available and consent remains unchanged

### Requirement: Apple sign-in interaction analytics catalogue

The design-system analytics catalogue SHALL cover Apple sign-in with the existing `ui_control_activated` event (`surface=sign_in`, `control_id=sign_in`) and cancellation `ui_recovery_selected` (`surface=sign_in`, `error_code=auth_cancelled`, `action=retry`). Delivered events MUST include a UUID identity and common properties, respect opt-out, and MUST NOT include Apple email, name, subject, authorization code, state, nonce, identity token, or learner text.

#### Scenario: Apple sign-in interaction

- **WHEN** a Learner starts Apple sign-in
- **THEN** the client emits the existing sign-in control event with only catalogued properties and common properties

#### Scenario: Apple OAuth cancellation

- **WHEN** a Learner cancels Apple sign-in
- **THEN** the client emits the catalogued cancellation recovery event only when analytics is enabled

### Requirement: Course switcher and per-course level controls
The client SHALL render the enrolled Course catalog matching canvases S03, S08, and S23 with current-course markers, progress indicators, confirmed edited titles, and ordered lesson goals. The Settings/Profile view SHALL provide per-course level controls to view, override, and reset each enrolled course's level. Course switching SHALL emit `ui_navigation_selected` and level updates SHALL emit `learner_profile_updated`, respecting analytics opt-out.

#### Scenario: Switching courses
- **WHEN** a learner selects another enrolled course
- **THEN** the selection persists, the path view updates to the selected course's outline, and `ui_navigation_selected` is emitted if opted in

#### Scenario: Override course level from settings
- **WHEN** a learner overrides or resets a course level
- **THEN** the course's level indicator updates without modifying the global profile

### Requirement: Lesson tutor sheet design parity
The lesson tutor sheet SHALL match canvas S19-Tutor and S09-Sheets: a docked panel on desktop and a modal sheet on mobile with the same controls, the context card, Stop, retry and credit recovery, in light, dark and reduced-motion states, with tokens winning over canvas values.

#### Scenario: Mobile
- **WHEN** the viewport is narrow
- **THEN** the tutor is a modal sheet with a labelled close control and a keyboard-reachable Stop

### Requirement: Save and delete design parity
Save as quick note, the saved status, the delete confirm and source-unavailable notes SHALL match canvas S19-Tutor and S09-Sheets on desktop and mobile, with loading, offline and error states, keyboard focus return, light/dark and reduced motion; tokens win over canvas values.

#### Scenario: Confirm dialog
- **WHEN** the Learner opens delete
- **THEN** a modal names the thread and message count, focus starts on Cancel, Escape cancels and returns focus to the opener

### Requirement: Approved placeholder brand identity components and assets
The client application SHALL provide dedicated accessible `BrandMark`, `BrandWordmark`, and composite `Brand` components utilizing the approved two-tile vector geometry and lowercase wordmark. The mark MUST remain visually legible at 16px and icon sizes. All vector assets SHALL use approved original geometry without third-party game likeness. The client MUST provide Git-backed app icons for Tauri and a web manifest and favicon in the web build.

#### Scenario: Mark rendering at small icon sizes
- **WHEN** the brand mark is displayed at 16px or navigation size
- **THEN** both geometric tiles maintain clear separation and token contrast

#### Scenario: Accessible brand header
- **WHEN** an assistive technology inspects the brand header
- **THEN** an accessible name "Duolify" is exposed and decorative tiles are hidden from screen readers

### Requirement: Optional feedback cues and settings
The client SHALL provide independent settings for UI sounds and haptics on the Settings screen (S23). UI sound cues SHALL remain silent no-ops in v1 without downloading audio assets over the network. Haptic feedback SHALL trigger only on user-initiated actions when supported by the platform, MUST NOT punish incorrect answers, and SHALL remain a separate opt-in control independently of reduced motion (reduced motion suppresses celebration movement only; completion keeps the short paired pulse when haptics are enabled).

#### Scenario: UI sound cue trigger with sound disabled or missing assets
- **WHEN** a user activates a UI control with sounds enabled
- **THEN** no network audio request is dispatched and playback completes silently

#### Scenario: Reduced motion decouples from haptic vibration
- **WHEN** the learner enables reduced motion or the operating system prefers reduced motion
- **THEN** celebration movement is suppressed while opt-in haptic vibration remains active according to the learner's haptic setting

### Requirement: Feedback and identity analytics events
The analytics catalogue SHALL include `ui_control_activated` with surface `settings` and control IDs `ui_sounds` and `haptics`. Telemetry MUST NOT emit repeated cues for purchase success or learner private text.

#### Scenario: Feedback setting toggle
- **WHEN** an opted-in learner toggles UI sounds or haptics
- **THEN** one `ui_control_activated` event is emitted with the corresponding control ID

### Requirement: Brand assets and feedback data backup
Brand assets and web manifest SHALL be Git-backed source files. Feedback preferences SHALL be persisted in `learner_profiles` within PostgreSQL, included in nightly encrypted dumps, and permanently deleted upon account erasure.

#### Scenario: Settings restored from dump
- **WHEN** a learner profile is restored from backup
- **THEN** saved UI sounds and haptics preferences are restored to their saved values

### Requirement: AI reliability boundary
The brand identity and feedback controls SHALL NOT invoke AI models or modify existing engine evaluation golden sets or fallback gates.

#### Scenario: Offline feedback
- **WHEN** the client is offline
- **THEN** brand assets render locally and feedback settings operate without AI provider contact
