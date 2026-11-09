## ADDED Requirements

### Requirement: Shared application shells
The client SHALL expose Learn, Courses, Friends, Leagues, Quick notes, Bookmarks, Credits, Profile and Settings through a sidebar at 48rem and five labelled bottom tabs below 48rem. More SHALL link secondary destinations and remain current on their routes. A support rail MAY appear at 75rem. Route, scroll and in-progress child state SHALL survive resize. Navigation SHALL reserve its rendered height and safe areas at 320px and 200% text.

#### Scenario: Mobile More navigation
- **WHEN** a learner opens Credits from More
- **THEN** existing pricing remains available, More is current, and browser Back returns to More

### Requirement: Focused and adaptive layout contracts
Lesson and test routes SHALL hide peripheral navigation and retain exit context and progress. Shared layout hosts SHALL map desktop panels to mobile modal sheets or steps and list/detail panes to separate mobile surfaces without duplicated child state. Modal focus SHALL be contained and restored. Pending, error and unavailable boundaries SHALL expose safe recovery. Navigation SHALL focus the route heading and provide a skip link.

#### Scenario: Resize during editing
- **WHEN** a desktop panel containing a draft becomes a mobile sheet or step
- **THEN** the same draft and actions remain, and closing restores focus to its trigger

### Requirement: Shell analytics catalogue
The catalogue SHALL include `ui_screen_viewed(screen_id)` and `ui_navigation_selected(destination)`. Shell emitters SHALL accept only allowlisted destination/screen identifiers, include UUID identity, platform, app_version and source, and suppress delivery after opt-out. No URL, route parameter, learner text or private query SHALL be delivered.

#### Scenario: Private deep link
- **WHEN** an opted-in learner navigates to a private object route
- **THEN** analytics contains only its screen enum and common properties, never its ID or query

### Requirement: Disposable shell state
Shell source SHALL be restored from Git. Navigation and layout state SHALL remain disposable memory state and create no new backup or deletion dependency. No new AI behavior SHALL bypass feature evaluation or fallback gates.

#### Scenario: Fresh host
- **WHEN** a shell or layout host is destroyed and recreated
- **THEN** ephemeral draft and navigation state are reset without deleting server-owned data
