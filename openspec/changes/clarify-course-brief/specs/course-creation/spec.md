## ADDED Requirements

### Requirement: Clarify and explicitly confirm a Course brief
The system SHALL collect an editable Course brief through one-question guided chat or an equivalent form before paid preview. Confirmed onboarding context MAY seed fields; skipped context MUST stay empty. Each field SHALL support free text, quick replies and Skip with at most one follow-up for vague answers. Include and skip topics MUST remain separate.

#### Scenario: Review on either screen size
- **WHEN** a learner clarifies their intent
- **THEN** desktop shows a docked editable brief and mobile shows an equivalent sheet with confirm and keep-chatting actions
- **AND** no preview, outline, enrollment, reservation or Course is created

#### Scenario: Assistant unavailable
- **WHEN** chat is disabled, offline or times out
- **THEN** all collected answers and the current answer are available in the equivalent form
- **AND** the learner can confirm without chat or a positive balance

### Requirement: Immutable authorized brief revisions
The API SHALL persist a learner-owned immutable revision only on explicit confirmation, with canonical hash and 30-day expiry. Changes after confirmation MUST invalidate the local confirmation and require a new revision. Reads MUST enforce ownership and expiry. Revisions SHALL enter account exports and encrypted general backups retained 14 days, cascade on erasure and honor restore tombstones. Session drafts MUST NOT be server-backed.

#### Scenario: Edit after confirmation
- **WHEN** a confirmed brief is edited
- **THEN** its local confirmation is cleared and the stored revision remains unchanged

#### Scenario: Another learner reads a revision
- **WHEN** a different authenticated learner requests the revision
- **THEN** the API returns not found without revealing the brief

### Requirement: Platform-paid development brief chat
Chat SHALL read course_brief_chat price, bounds and platform caps from Pricing config. Development SHALL allow the explicitly unapproved zero-credit config independently of launch approval. Production MUST require approved config and evaluated model promotion; the form MUST remain usable. Chat MUST NOT deduct learner Credits and SHALL be rate limited.

#### Scenario: Production gate closed
- **WHEN** the unapproved development config is selected in production
- **THEN** chat is disabled and form entry remains available

### Requirement: Brief chat analytics and reliability
The system SHALL use existing analytics wrappers with authenticated UUID, common properties and opt-out checks, never learner text. The catalogue SHALL include confirm_brief, course_brief sheet opening and existing guided controls. Every provider attempt SHALL report actual usage where available. Course brief chat SHALL enforce configured deadlines below API deadlines, token, attempt and USD caps, validate model schema deterministically and fall back to form on terminal failure. Offline Golden set cases SHALL exercise the production brief processor under mock providers and pass engine:eval; live publication remains gated by ADR 0011 provisioning.

#### Scenario: Invalid provider output
- **WHEN** the model returns an invalid schema
- **THEN** bounded attempts end in the form fallback with the draft retained

#### Scenario: Analytics opted out
- **WHEN** a learner disables analytics
- **THEN** brief controls and model attempts send no PostHog event
