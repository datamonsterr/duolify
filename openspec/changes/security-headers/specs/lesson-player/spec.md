## ADDED Requirements

### Requirement: Sandbox host page with its own policy
The client SHALL play each Slide in `<iframe sandbox="allow-scripts">` (never `allow-same-origin`) whose `src` is the sandbox host page `/lesson-frame.html`, not a `srcdoc`, so that the edge can give that page a Content-Security-Policy of its own. The page MUST accept the Slide document only from its parent by `postMessage` and write it. Its policy MUST allow inline script and style but no network except the asset origin, no frames, forms or workers, and `frame-ancestors 'self'`. The application's own policy MUST NOT allow `unsafe-inline` scripts to make Slides run. The page SHALL be served `immutable` under a per-build URL so a Lesson replays offline from the HTTP cache.

#### Scenario: Lesson plays under the strict app policy
- **WHEN** the web client runs with the production Content-Security-Policy and the Learner opens a Lesson
- **THEN** the Slide's inline script runs in the sandbox frame, responds to player commands and the browser logs no policy violation

#### Scenario: Slide cannot reach the app
- **WHEN** a Slide script reads `parent.localStorage` or fetches a third-party origin
- **THEN** the sandbox and the frame policy block it and playback continues

#### Scenario: Next slide while offline
- **WHEN** the connection is gone and the Learner moves to the next Slide of an opened Lesson
- **THEN** the sandbox host page loads from the HTTP cache and the Slide plays
