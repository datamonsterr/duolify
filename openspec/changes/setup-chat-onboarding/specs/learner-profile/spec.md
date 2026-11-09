## ADDED Requirements

### Requirement: Guided conversational setup chat
The API SHALL expose an authenticated `POST /v1/me/setup-chat` endpoint backed by the stateless Engine `SetupChat` RPC. The chat SHALL guide the Learner through onboarding questions covering goal, topic, known background/level, daily time/XP goal, reminders, and detected confirmable time zone. Every question MUST be skippable. Only a vague answer SHALL trigger one clarifying follow-up question per step; subsequent answers for that step MUST NOT trigger a second follow-up. An editable live summary SHALL update as the Learner answers. No profile write SHALL occur before explicit confirmation. If the assistant fails, times out, or is cancelled, collected answers MUST be kept intact and the equivalent setup form MUST be opened. The setup chat SHALL also be accessible from the Profile screen with existing values prefilled.

#### Scenario: Vague goal answer triggers single follow-up
- **WHEN** the Learner answers a vague goal such as "something about space"
- **THEN** the assistant asks one clarifying follow-up question with quick replies and extracts the tentative topic

#### Scenario: Second answer does not trigger further follow-up
- **WHEN** the Learner provides an answer to the clarifying follow-up question
- **THEN** the assistant proceeds to the next setup step without asking another follow-up

#### Scenario: Skipped question preserves existing field
- **WHEN** the Learner selects Skip for an onboarding question
- **THEN** that field remains unchanged or empty, no value is invented, and the assistant advances to the next step

#### Scenario: Assistant failure falls back to form with answers intact
- **WHEN** the setup chat encounters a provider error or timeout
- **THEN** the client displays the form fallback keeping all collected answers editable without loss

### Requirement: Setup chat analytics events
The system SHALL emit PostHog analytics events during guided setup chat. The client SHALL emit `ui_control_activated` with surface `guided_chat` and control ID in `quick_reply|skip|form_fallback|confirm_summary`. The API SHALL emit `$ai_generation` for each assistant completion (recording model, provider, tokens, latency, and fallback status, but NEVER recording Learner free text or prompt content). Canonical profile save upon confirmation SHALL emit `learner_profile_updated` (with `fields_changed` names only) and `onboarding_completed` exactly as specified in E1-03. An opted-out Learner MUST NOT produce any PostHog events.

#### Scenario: Control activation analytics
- **WHEN** a Learner taps a quick-reply button or skips a question
- **THEN** `ui_control_activated` is emitted with the corresponding control ID and no private answer text

#### Scenario: Privacy and opt-out
- **WHEN** an opted-out Learner participates in setup chat
- **THEN** neither UI nor AI generation events are sent to PostHog

### Requirement: Setup chat data backup and retention
Unconfirmed setup chat transcripts and live summaries SHALL remain ephemeral session drafts in device memory and MUST NOT be persisted on the server, in the database, in the Engine, or in analytics. Only the confirmed profile/preferences SHALL be persisted to Postgres `learner_profiles` through the canonical E1-03 endpoint, covered by 14-day nightly encrypted backups, export, and account deletion cascades within 30 days.

#### Scenario: Abandoned unconfirmed setup chat
- **WHEN** a Learner navigates away from or closes unconfirmed setup chat
- **THEN** no unconfirmed profile data or transcript is persisted on the server

### Requirement: Setup chat AI reliability and evals
The setup chat feature SHALL include golden-set cases in `evals/fixtures/` covering vague versus specific answers, the one-follow-up bound, skipped answers, non-English answers, prompt injection resistance, summary extraction fidelity, no profile save before confirmation, and assistant failure form recovery. These cases MUST pass `mise run engine:eval` against the required threshold. The Engine MUST enforce token limits, schema validation, and config timeouts below API deadlines. In case of provider failure, the Engine router MUST attempt one fallback before returning a typed error that triggers form fallback.

#### Scenario: Evaluation against golden set
- **WHEN** `engine:eval` runs in offline mock mode
- **THEN** all `setup_chat` cases pass deterministic checks and meet the minimum score threshold
