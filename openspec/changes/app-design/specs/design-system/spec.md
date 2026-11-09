## ADDED Requirements

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
