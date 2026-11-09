## ADDED Requirements

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
