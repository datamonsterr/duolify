## ADDED Requirements

### Requirement: Offline lesson playback does not depend on install-time precache
The public service worker SHALL precache on install only the files the first screen needs. It SHALL cache the lesson-player, lesson-test and course-list route chunks when a lesson is opened or a course library is downloaded, keep them cached across service-worker versions once cached, and cache any other hashed asset on first use. A lesson that was opened or downloaded SHALL play after the device goes offline and the app is reloaded.

#### Scenario: Opened lesson restarts offline
- **WHEN** a learner opens a lesson online, goes offline and reloads
- **THEN** the shell, the lesson-player chunk and the cached lesson bundle load without network and every slide plays

#### Scenario: Chunks of unvisited routes are not precached
- **WHEN** the service worker installs
- **THEN** route chunks the first screen does not need (for example the marketplace) are not downloaded until first used

#### Scenario: Route chunk fails to load
- **WHEN** a route chunk cannot be fetched (offline, or replaced by a newer deploy)
- **THEN** the page shows an error with Retry, and Retry loads the chunk once it is reachable

### Requirement: First-load budget
The built client SHALL keep the JavaScript and CSS loaded by `index.html` within the limits in `apps/client/bundle-budget.json`, and CI SHALL fail when they are exceeded.

#### Scenario: Budget exceeded
- **WHEN** a change grows first-load gzip JavaScript past the budget
- **THEN** `mise run client:bundle-budget` fails and names the exceeded limit
