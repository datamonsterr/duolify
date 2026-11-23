## ADDED Requirements

### Requirement: Scripted guided chat replaces form filling
Every task flow in the client inventory below SHALL be presented as a guided chat: the guide asks scripted questions as chat bubbles and the Learner answers with inline widgets. Scripted questions MUST be deterministic, defined in client-bundled versioned scripts, and MUST NOT call a model or spend Credits. Flows marked "stays" in the inventory MUST remain lists or forms. Screens MUST NOT present a multi-field form as the primary way to complete an inventoried flow once its flag is enabled.

| Current form | Script id | Widgets | Submits to (unchanged) |
|---|---|---|---|
| `SignIn` (email, code, age and consent checkboxes) | `auth.signin` | secure email, secure one-time code, confirm card with checkboxes | existing email-code sign-in endpoints |
| `BirthDatePrompt` | `auth.age` | date | existing age gate endpoint |
| `Welcome`, `SetupChat` | `onboarding.setup` | chips+text, single, stepper, toggle, time, confirm | `PUT /v1/me/profile` |
| `LearnerContextEditor` | `profile.edit` | same steps, prefilled, jump-to-step chips | `PUT /v1/me/profile` |
| `CourseBrief` (and `CourseCreate` inputs) | `course.brief` | chips+text, multi-select (include), multi-select (skip), single (level), confirm | brief confirmation endpoint |
| `CourseLevelOverride` | `course.level` | single, confirm | per-course level endpoint |
| `OutlineChat`, `InstructionPanel` (Course steering) | `course.steer` | single (scope), long text, confirm; AI outline proposal stays quoted | existing steering and outline proposal endpoints |
| `CreateCharacter`, `CharacterLibrary`, `CourseCast` | `character.create`, `character.cast` | short text, chips, long text, single, confirm | existing `custom-characters` endpoints (selectors reused, see mascot spec) |
| `SocialIdentity`, `AddFriend` | `social.username`, `social.addfriend` | short text, confirm | existing social endpoints |
| `ShareCourse`, `InviteFriends`, `RepublishPanel` | `course.share`, `course.invite`, `course.republish` | single, multi-select friends, long text, confirm | existing sharing endpoints |
| `CourseReviews` | `course.review` | rating, long text, confirm | existing review endpoint |
| `ReportSheet`, `ReasonDialog` | `report.content` | single (reason), long text, confirm | existing report and appeal endpoints |
| `PricingView`, `QuotePanel`, `PlanSummary` | `credits.confirm` | single (pack or plan), confirm card (Action quote, consent) | existing quote and checkout endpoints; payment page stays hosted |
| `AccountPrivacy` export | `account.export` | confirm card | existing export endpoint |
| `AccountPrivacy` delete | `account.delete` | destructive confirm card, secure password | existing erasure endpoint |
| `DailyGoal`, reminder fields of `Settings` | `goal.daily`, `reminder.edit` | single, stepper, toggle, time | existing settings endpoint |
| Pricing admin forms | stays | n/a | operator forms are out of scope |
| `Settings` toggles/selects, `PrivacySettings`, `PreferenceControls`, `VoicePanel` | stays | settings list | existing settings endpoints |
| Quick note and notes editing, Tutor chat composer, marketplace search, lesson test `AnswerPanel`, analytics filters | stays | direct input, not form filling | unchanged |

#### Scenario: Inventoried flow opens as chat
- **WHEN** a Learner starts onboarding, a course brief, sign-in or any other inventoried flow with its flag enabled
- **THEN** the guide asks the first question as a chat bubble and no multi-field form is shown

#### Scenario: Scripted step costs nothing
- **WHEN** the Learner answers a scripted question
- **THEN** no model call is made, no Credits are reserved or spent, and the next question appears immediately

#### Scenario: Stays flows unchanged
- **WHEN** the Learner opens Settings or an operator opens pricing admin
- **THEN** a list or form is shown and no guided chat starts

### Requirement: Script definition format
A script SHALL be a typed, versioned declarative module with: stable `id` and integer `version`; ordered steps each with stable `id`, `widget`, EN and AR i18n keys for prompt, options and errors, `validate` rules mirroring the API limits, `skippable`, `persist` (false for secrets), optional `when` and `next` branches written in a closed expression language (`eq`, `in`, `answered`, `not`; no code evaluation), an analytics id, and a final `submit` naming the existing endpoint. CI MUST fail when a locale key is missing, a branch target does not exist, a validator is looser than the API limit, or a secure step is marked persistent.

#### Scenario: Missing Arabic key
- **WHEN** a script references a key absent from the Arabic bundle
- **THEN** the build check fails and names the key

#### Scenario: Secure step persisted
- **WHEN** a password, one-time code or destructive confirm step sets `persist` true
- **THEN** the build check fails

### Requirement: Inline answer widgets
The chat SHALL provide these widgets inside the conversation: single-choice chips, multi-select checkboxes, time picker (clock), date, number stepper or slider, toggle, short text, long text, image or file pick, rating, confirm card, and secure inputs (email, one-time code, password). Each widget MUST be a native, labelled form control or a standard composite of them, MUST validate on the client with the same limits as the API, MUST show an inline error without losing the entry, and MUST offer Skip when the step is skippable. Secure inputs MUST be real `<input>` elements with correct `type`, `name`, `autocomplete` (`email`, `one-time-code`, `current-password`, `bday`) and `inputmode`, inside a one-field form in the Learner slot so password managers, OTP autofill and screen readers work. Secrets and codes MUST NOT be echoed as bubbles or stored in drafts.

#### Scenario: One-time code autofill
- **WHEN** the code step appears on a device that offers an SMS or email code
- **THEN** the field carries `autocomplete="one-time-code"` and accepts the suggested value

#### Scenario: Invalid time
- **WHEN** the Learner submits an invalid reminder time
- **THEN** an inline labelled error appears, the entry is kept and the step is not advanced

### Requirement: Editable answers
Each answer SHALL appear as the Learner's bubble. Activating a bubble (tap, click, Enter or Space) MUST reopen that step's widget with the current value. After an edit the script MUST re-evaluate branches: later answers that are still valid are kept, answers that became invalid or unreachable are dropped and re-asked, and the guide MUST list what changed. Editing MUST NOT submit anything before the final confirm step.

#### Scenario: Edit keeps valid later answers
- **WHEN** the Learner edits an earlier answer that does not affect later steps
- **THEN** all later answers remain and the Learner returns to the step they were on

#### Scenario: Edit invalidates a branch
- **WHEN** an edit makes a later answer unreachable
- **THEN** that answer is dropped, the guide says which one, and only the new steps are asked

### Requirement: Implicit progress, resume and offline
The chat MUST NOT display numeric step counters or "step n of m". Progress MAY be an aria-hidden ambient bar and the guide MAY say a step is the last. Answers MUST be saved after each step to the per-Learner draft and a local copy. After an app restart or on another device the guide MUST resume at the first unanswered step, restoring answered bubbles. Scripts, FAQ and the mascot MUST work offline; steps that need the network or Credits MUST keep answers and show an offline state; idempotent free submits MAY queue and retry on reconnect. A draft whose script version changed MUST migrate matching answers or restart with a notice.

#### Scenario: Resume after restart
- **WHEN** a Learner closes the app on the third question and reopens it
- **THEN** the first three bubbles are restored and the guide asks the fourth question

#### Scenario: Offline submit
- **WHEN** the Learner reaches a network step offline
- **THEN** the answers are kept, the guide explains the connection is needed, and no data is lost

### Requirement: Free text in between
At any step the composer SHALL accept free text. The client MUST first apply the input guardrail from `prompt-skill-routing-and-guardrails`, then a deterministic prefilter (commands back, skip, help, start over; text that validates as the current step's answer), then a cheap intent check returning a closed route `answer`, `question`, `faq`, `command` or `off_topic`. `answer` fills the step as an editable bubble; `faq` answers from the static bundled FAQ for free; `question` is answered by the tutor or guide with an Action quote when it costs Credits; `command` runs only non-destructive commands; `off_topic` gets a one-line redirect. After any detour the guide MUST re-ask the same step. Free text MUST NOT change the script, answer a secure step, or confirm a destructive or paid step.

#### Scenario: Question mid-script
- **WHEN** the Learner types "Why do you need my level?" on the level step
- **THEN** a free FAQ answer is shown and the level question is asked again

#### Scenario: Destructive step cannot be confirmed by text
- **WHEN** the Learner types "yes delete it" on the account deletion confirm card
- **THEN** nothing is deleted and the card still requires the explicit confirm button

#### Scenario: Answer typed instead of chosen
- **WHEN** the Learner types "7:30" on the reminder time step
- **THEN** it is accepted as the answer in an editable bubble

### Requirement: Credits, quotes, rate limits and guardrails for free text
Scripted steps, commands and FAQ answers MUST be free. A `question` answered by AI MUST show the existing Action quote inline before running, MUST draw from Daily free credits first, MUST never auto-spend, and declining MUST return to the step. Intent-check calls are platform-paid, capped per Learner and by USD, and never charged to the Learner. Free text MUST be limited to 10 turns per minute and 40 per script run per Learner; beyond the limit the guide offers buttons only. Learner text MUST be treated as data and MUST NOT reach a system prompt; blocked input gets a neutral reply and the step is re-asked.

#### Scenario: Paid answer needs consent
- **WHEN** a question needs an AI answer that costs Credits
- **THEN** a quote card is shown, nothing runs until the Learner accepts, and declining resumes the step

#### Scenario: Rate limit
- **WHEN** a Learner sends more than 10 free-text turns in a minute
- **THEN** the guide stops routing and offers the step's buttons until the window clears

### Requirement: Classifier fallback and reliability
If the intent check times out (3 s), errors or is below confidence, the client MUST fall back deterministically: treat text that validates as a step answer as that answer, otherwise show buttons "Use as my answer", "Ask the guide" and "Skip". The `guided_intent` task MUST have Golden set cases in the Hugging Face dataset covering the five routes, mixed-language (EN and AR) text, destructive-intent phrases, prompt injection and PII; `mise run engine:eval` MUST enforce route accuracy at least 0.92, destructive false-command rate 0 and injection resistance at least 0.99. Live promotion follows ADR 0019 and ADR 0006 provider fallback, timeouts and cost caps.

#### Scenario: Classifier down
- **WHEN** the classifier is unavailable
- **THEN** the flow continues with the deterministic fallback and no AI spend

### Requirement: Accessible chat semantics
The thread MUST use `role="log"` with polite announcements of each completed message once per stable id; streaming text MUST stay outside live regions. Widgets MUST be real controls with labels tied to the guide's question, grouped in fieldsets with legends. The flow MUST be fully keyboard operable (Tab, arrow keys within groups, Enter to send, Esc to cancel an edit or close a sheet). Focus MUST move to the first control of each new question and MUST NOT be stolen from a field being edited. There MUST be no timer, expiry or auto-advance while the Learner answers. Reduced motion (OS or setting) MUST remove typewriter effects, mascot motion and auto-scroll and offer a jump-to-latest control. Touch targets MUST be at least 44 px and colour pairs MUST come from the token contrast table.

#### Scenario: Screen reader flow
- **WHEN** a screen reader user answers a single-choice step
- **THEN** the question is announced once, the options are a labelled radio group, and the next question is announced after Enter

#### Scenario: Keyboard only
- **WHEN** a keyboard user completes onboarding without a pointer
- **THEN** every step, edit, skip and confirm is reachable and Esc cancels an edit

### Requirement: Server validation and API contract unchanged
Client validation MUST be presentation only. Final submits MUST use the existing endpoints with unchanged request and response contracts, and the server MUST reject invalid values exactly as before. The API SHALL add owner-only `GET|PUT|DELETE /v1/me/guided-drafts/{script_id}` (body `script_version`, `step_id`, bounded `answers`, 16 KB cap, `If-Match` on `updated_at`, 404 for other Learners) and `POST /v1/me/guided-chat/route` (rate limited, no text persistence). No proto change is part of this change.

#### Scenario: Server rejects a bad value
- **WHEN** a client submits a profile value outside the API limits
- **THEN** the API rejects it unchanged and the guide re-opens the step with the error

#### Scenario: Cross-device draft conflict
- **WHEN** a draft is saved with a stale `If-Match`
- **THEN** the API returns a conflict and the client merges per step before retrying

### Requirement: Guided chat analytics
The system SHALL emit content-free PostHog events `guided_chat_started`, `guided_chat_resumed`, `guided_chat_step_answered`, `guided_chat_step_edited`, `guided_chat_free_text_asked`, `guided_chat_abandoned` and `guided_chat_completed`. Properties: `script_id`, `step_id`, `widget_type`, `entry_point` (feature attribution, never the common `source`), plus `skipped`, `route` (`answer`, `question`, `faq`, `command`, `off_topic`, `fallback`), `charged` (boolean), `duration_ms` bucket and `free_text_count`. Learner text, FAQ ids with content and secret values MUST NOT be sent. Common properties and opt-out rules apply. The funnel MUST report completion rate versus the old form (A/B by Learner id for 14 days per flow), time to complete, drop-off step and free-text rate.

#### Scenario: Opt-out
- **WHEN** a Learner has disabled analytics
- **THEN** no guided chat event is sent

#### Scenario: Content-free step event
- **WHEN** a goal step is answered
- **THEN** the event carries `script_id`, `step_id` and `widget_type` but not the goal text

### Requirement: Guided chat draft backup, retention and erasure
Drafts SHALL live in Postgres table `guided_chat_drafts` (learner, script, version, step, bounded answers, updated, expires), excluding secrets. Rows expire after 30 days idle and are purged by the worker. Drafts MUST be in encrypted nightly dumps (14 days), in the account export, and cascade on account erasure. The restore drill MUST verify the table restores and that expired rows and erased accounts are purged after restore. Client local copies MUST be cleared on sign-out and on completion.

#### Scenario: Erasure
- **WHEN** a Learner deletes their account
- **THEN** their drafts are removed and are absent after a restore replay

#### Scenario: Idle expiry
- **WHEN** a draft is untouched for 30 days
- **THEN** it is purged and the flow starts fresh next time
