## ADDED Requirements

### Requirement: Controlled resumable playback
The player SHALL provide play, pause, replay, seek, rate, previous and next using a parent clock and sandbox bridge. It SHALL handle only exact-schema active-frame ended/checkpoint messages, ignore stale frames, preserve sandbox allow-scripts without allow-same-origin and strict CSP, pause on hidden pages and sheets, and restore focus on sheet closure. Exit SHALL return to a validated originating path. Ended SHALL expose explicit next/test controls without completing a Lesson. Desktop and mobile SHALL have keyboard/touch, light/dark and reduced-motion parity.

#### Scenario: Pause seek rate and resume
- **WHEN** a learner pauses, seeks, changes speed, exits and reopens
- **THEN** slide and bounded time restore paused, and explicit Play continues through the same playback controls

#### Scenario: Hidden sheet or stale frame
- **WHEN** the page hides or a sheet opens, or a replaced frame posts ended/checkpoint
- **THEN** playback pauses with time retained or the stale event is ignored, and completion is never granted

### Requirement: Learner position durability and recovery
The API SHALL authorize cursor reads/writes by enrollment and validate manifest version, slide and integer time bounds. Cursors SHALL persist in Postgres, join encrypted nightly 14-day dumps, learner export, cascade deletion and restored deletion-tombstone replay. Learner-scoped disposable local cursors SHALL clear on logout. Offline or save errors SHALL offer understandable recovery without claiming synchronization. No AI behavior changes or new eval cases are introduced; existing reliability gates SHALL remain intact.

#### Scenario: Restore and erase cursor
- **WHEN** a backup is restored and learner deletion tombstones are replayed
- **THEN** the original cursor restores before deletion and is erased afterwards

#### Scenario: Save unavailable
- **WHEN** saving position fails or the device is offline
- **THEN** the learner retains local position, sees an unsaved recovery state and can retry or exit

### Requirement: Control analytics catalogue
Before emitters, the API allowlist and analytics catalogue SHALL register slide_viewed (lesson_id, slide_id, index, watch_ms, offline), slide_replayed and slide_paused (lesson_id, slide_id, index), and lesson_player_error with fixed codes including position_unavailable. Authenticated UUID/common properties and opt-out SHALL apply. No slide/learner text, HTML or URL SHALL be emitted. slide_viewed SHALL emit only on ended, at most once per playback attempt, using measured playing wall time and offline state. Playback SHALL never emit lesson_completed.

#### Scenario: Viewed and replayed
- **WHEN** a slide ends, duplicate ended arrives and the learner replays
- **THEN** one slide_viewed emits for that attempt, replay emits slide_replayed, and seeking or mounting alone emits no view

#### Scenario: Consent and private text
- **WHEN** consent is disabled or a caller adds private properties
- **THEN** the PostHog test double receives no event or only the closed registered properties
