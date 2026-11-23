## MODIFIED Requirements

### Requirement: Clarify and explicitly confirm a Course brief
The system SHALL collect an editable Course brief through the scripted guided chat `course.brief` (no model call, no Credits) before paid preview. Confirmed onboarding context MAY seed answers; skipped context MUST stay empty. Each field SHALL support its widget (chips with text, multi-select, single choice), free text under the `guided-chat` routing rules, and Skip. Include and skip topics MUST remain separate answers. There is no equivalent form once the flag is enabled; the chat is the only entry.

#### Scenario: Review on either screen size
- **WHEN** a learner finishes the brief questions
- **THEN** desktop shows a docked editable brief and mobile shows an equivalent sheet with confirm and keep-chatting actions
- **AND** no preview, outline, enrollment, reservation or Course is created

#### Scenario: Offline or flag off
- **WHEN** the device is offline while answering
- **THEN** all answers are kept in the draft and confirmation waits for the connection without needing a positive balance

### Requirement: Immutable authorized brief revisions
The API SHALL persist a learner-owned immutable revision only on explicit confirmation, with canonical hash and 30-day expiry. Changes after confirmation MUST invalidate the local confirmation and require a new revision. Reads MUST enforce ownership and expiry. Revisions SHALL enter account exports and encrypted general backups retained 14 days, cascade on erasure and honor restore tombstones. In-progress answers MAY be stored as `guided_chat_drafts` under the `guided-chat` retention rules (30 days idle, erasure cascade) and are never a revision.

#### Scenario: Edit after confirmation
- **WHEN** a confirmed brief is edited
- **THEN** its local confirmation is cleared and the stored revision remains unchanged

#### Scenario: Another learner reads a revision
- **WHEN** a different authenticated learner requests the revision
- **THEN** the API returns not found without revealing the brief

#### Scenario: Draft is not a revision
- **WHEN** a Learner answers brief questions but does not confirm
- **THEN** only a draft exists and no revision is created

### Requirement: Platform-paid development brief chat
The scripted brief questions SHALL be free and need no Pricing config key. Only optional AI help (a free-text question answered by the tutor or guide) uses the existing Action quote and Pricing config of the tutor chat path, and the intent check is platform-paid with caps from Pricing config and rate limits. Production MUST require approved config and evaluated model promotion for those AI parts only; the scripted brief MUST always work. The `course_brief_chat` price is no longer charged for scripted questions.

#### Scenario: AI help gate closed
- **WHEN** model promotion for free-text answers is not approved in production
- **THEN** the scripted brief still works, FAQ answers still work and AI questions show an unavailable notice

## ADDED Requirements

### Requirement: Outline changes and steering as guided chat
Course steering and outline change requests SHALL start as the scripted `course.steer` chat (scope choice, instruction text, confirm). The outline change proposal produced from the instruction remains an AI result with the existing Action quote, immutable proposal and explicit apply, unchanged by this change.

#### Scenario: Steering scope
- **WHEN** the Learner chooses "rest of course" and types an instruction
- **THEN** the confirm card shows the scope and text before it is saved as a Course instruction
