## ADDED Requirements

### Requirement: Free accessible sample outline editing
Learners SHALL reorder units and lessons using drag handles or move buttons, rename, edit goals, add/remove items and undo. IDs SHALL remain stable, order SHALL follow arrays and lessons SHALL have exactly one parent. Position announcements and focus SHALL remain on moved items. Mobile and desktop SHALL expose identical operations with token-based light/dark and reduced-motion presentation. Manual editing and undo MUST NOT call a model or spend Credits.

#### Scenario: Undo after reload
- **WHEN** a learner saves edits and reloads the saved outline
- **THEN** the same IDs, order, content and last 50 pre-edit snapshots are restored
- **AND** Undo restores the preceding snapshot

### Requirement: Authorized draft revision concurrency
The API SHALL validate ownership, schema and expected_revision before saving or confirming. Invalid saves SHALL preserve client edits. Stale saves SHALL preserve the current server head and persist the submitted draft as a recovery copy for explicit choice. Drafts and bounded history SHALL join account export, account/brief deletion and encrypted nightly 14-day general backups with restore tombstones applied.

#### Scenario: Concurrent stale save
- **WHEN** two clients save the same expected revision
- **THEN** one advances the head and the other receives a conflict with both saved branches
- **AND** neither client silently replaces the other branch

### Requirement: Confirm exact outline snapshot without generation
Explicit confirmation SHALL expose draft ID, revision, canonical hash and exact outline snapshot for later course creation and proposal validation. Any later edit SHALL invalidate current confirmation. Confirmation MUST NOT create a Course, enroll, call the engine or reserve Credits. UI events SHALL use the existing private-text-free allowlisted analytics wrapper and catalogue controls outline_move, outline_rename, outline_remove, outline_add and outline_undo, respecting opt-out.

#### Scenario: Stale consumer
- **WHEN** a consumer requests a confirmed revision after the draft changed
- **THEN** the API rejects it and requires explicit confirmation of the newer revision
