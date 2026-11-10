## ADDED Requirements

### Requirement: Authored shared discovery example
The system SHALL serve a fixed safe versioned authored sample outline and Slides at zero balance without engine/provider calls, Course or enrollment creation, reservations or Credit grants. The shared example SHALL be clearly distinguished from a personalized paid preview. Live catalogue activation SHALL remain gated by E12-08; development configuration SHALL remain unapproved.

#### Scenario: Inspect a shared example
- **WHEN** a visitor or Learner opens the shared example from Welcome or Create
- **THEN** the authored outline and sandboxed Slides are displayed without personal input or paid generation events
- **AND** no Course, enrollment, quote, reservation or grant is written

### Requirement: Preserve creation while inspecting the demo
The client SHALL preserve guided brief answers, composer and edited creation drafts when entering or leaving the shared demo. Returning to personalized creation SHALL require a fresh quote and explicit consent before any paid preview work.

#### Scenario: Resume personalized creation
- **WHEN** a Learner returns from the shared example
- **THEN** their Brief → Preview → Outline → Create draft remains available
- **AND** previous quote consent cannot start a personalized preview

### Requirement: Shared demo analytics and recovery
The catalogue SHALL include course_preview_viewed with preview_kind=shared_demo and demo_version, and ui_control_activated with control_id=shared_demo and surface=welcome|course_brief. Events SHALL use UUID/common properties, respect opt-out and contain no learner text. Shared demo assets SHALL be Git-backed and covered by checksum-verified versioned object-storage restore fixtures. Existing AI eval/fallback gates SHALL remain unchanged.

#### Scenario: Demo at zero balance or offline
- **WHEN** a Learner with zero balance opens the demo or its download fails
- **THEN** no paid event or credit effect occurs and the client offers Return and Retry without losing drafts

#### Scenario: Restore authored assets
- **WHEN** current demo objects are lost
- **THEN** the protected versioned copy restores the exact Git-authored bundle without AI generation or learner data
