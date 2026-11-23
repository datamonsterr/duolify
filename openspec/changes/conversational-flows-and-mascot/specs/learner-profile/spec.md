## MODIFIED Requirements

### Requirement: Guided conversational setup chat
Onboarding setup SHALL be the scripted guided chat `onboarding.setup` defined by the `guided-chat` capability, with no model call and no Credits. It SHALL ask about goal, topic, known background/level, daily time/XP goal, reminders (with the clock widget) and a detected, confirmable time zone. Every question MUST be skippable and a skipped question MUST leave its field unchanged or empty without inventing a value. An editable summary card MUST update as the Learner answers. No profile write SHALL occur before explicit confirmation, which submits to the existing `PUT /v1/me/profile` unchanged. The Learner MAY type free text at any step under the `guided-chat` free-text routing rules. The same script, prefilled with existing values and with jump-to-step chips, SHALL serve as profile edit from the Profile screen. The AI-asked `POST /v1/me/setup-chat` path and the vague-answer follow-up are retired; the Engine `SetupChat` RPC becomes unused and is removed in a later matching API, Engine and proto change.

#### Scenario: Skipped question preserves existing field
- **WHEN** the Learner selects Skip for an onboarding question
- **THEN** that field remains unchanged or empty, no value is invented, and the guide advances to the next step

#### Scenario: Draft is not a write
- **WHEN** the Learner leaves setup before confirming the summary
- **THEN** the profile is unchanged and the answers remain in the draft for resume

#### Scenario: Profile edit prefilled
- **WHEN** the Learner opens profile edit from the Profile screen
- **THEN** the guide opens with existing values filled and the Learner can jump to any step

#### Scenario: Free question during setup
- **WHEN** the Learner types a question on a setup step
- **THEN** the free-text routing answers it per the `guided-chat` rules and the step is asked again

## ADDED Requirements

### Requirement: Settings are a list, reminders and goals are guided
Appearance, motion, narration, narration speed, captions, text size, UI sounds, haptics, privacy toggles, voice choice and locale SHALL remain a settings list with the existing persistence and validation. Daily goal and reminder time SHALL be editable through the `goal.daily` and `reminder.edit` guided scripts, and MAY also appear in the list as read-only summaries linking to those scripts.

#### Scenario: Toggle a setting
- **WHEN** the Learner changes text size in Settings
- **THEN** it applies and persists without starting a guided chat

### Requirement: Guided setup analytics, backup and reliability
Setup SHALL emit the `guided-chat` analytics events with `script_id=onboarding.setup` and `entry_point` `onboarding` or `profile`, with no profile text. Drafts follow the `guided_chat_drafts` backup, retention and erasure rules; confirmed profile data keeps existing rules. Because setup uses no model, the setup chat Golden set cases move to the `guided_intent` task for free-text routing; setup keeps no other AI reliability burden.

#### Scenario: Funnel comparison
- **WHEN** the A/B window closes
- **THEN** completion rate and time to complete for the chat are compared with the old flow from the same event catalogue
