# Guided flow components (C14–C17)

Import primitives and types from `features/design/chat`. The separate
`/guided-chat.html` page demonstrates authored fixtures without application routes
or API dependencies. `mise run client:e2e-guided` runs its browser checks;
`mise run client:test-guided` runs component tests.

- `GuidedChat`: controlled `messages`, `draft`, current `question` and callbacks.
  Keep assistant message IDs stable while streaming, then set `status: "complete"`
  once. The hidden polite log announces completion once per ID. Quick replies are
  selections, submitted alongside free text by Send; `multiple` allows any number.
  The caller owns question progression, follow-ups, Skip semantics and form fallback.
- `LiveSummary`: controlled `lines` and explicit `onConfirm`; errors are associated
  with their fields. Consumers own validation, dirty/pending/saved state and consent
  before paid operations. Pass the same state into the fallback form.
- `OutlineEditor`: initializes from `initialValue`, returns edits and Undo through
  `onChange`. A new `proposal` snapshot is applied as one undoable edit **only after**
  the caller approves it through `ProposalDiff.onApply`. Set the owning outline
  snapshot in that same handler. Keep unit/lesson IDs stable and unique; remount
  with a course-specific React key when changing courses. Reorder is within each
  sibling list, by handles, arrow keys or move buttons.
- `ProposalDiff`: presentation and Apply/Discard callbacks for labelled changes;
  the feature validates/applies the proposal to its own data. E13 fixtures contain
  authored examples, not an AI or diff engine.
- `ResponsivePanel`: mounts one subtree as a desktop panel or mobile sheet at
  48rem. `Sheet` also works alone. Optional `onPausePlayer` pauses the host when a
  modal opens; closing leaves resuming playback to the learner. Avoid stacked
  modals. `returnFocusRef` can supply an explicit trigger when needed.
- `Field`, `Feedback`, `UndoNotice`: labelled inputs, semantic status/error banners
  and persistent Undo feedback. Persistent notices have no automatic timeout.

Components emit allowlisted UI enums via the shared analytics context. Never pass
learner text as analytics identifiers. Auth supplies the UUID and consent; fixtures
configure no identity. Feature tickets own AI calls, persistence, credit outcomes,
API domain events, evaluation cases and timeout/fallback/refund handling.
