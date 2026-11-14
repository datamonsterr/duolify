## ADDED Requirements

### Requirement: Eligible versioned reviews
The API SHALL require a session, configured completed copy lessons (default three), no self-review, one review per learner/course, stars 1–5, at most 2000 Unicode characters and 20 submissions per rolling day. Edits SHALL preserve the reviewed version and update aggregates; deletion SHALL remove the contribution.

#### Scenario: Too early
- **WHEN** a learner has completed one copy lesson
- **THEN** submission is refused with the remaining lesson count

### Requirement: Fail-closed screened text
ScreenReview SHALL use the cheap evaluated route, strict schema, a shared ten-second deadline, at most one fallback and bounded platform cost. Only pass SHALL expose text; needs_review, block and pending_screening SHALL hide text while retaining stars.

#### Scenario: Abusive or unavailable
- **WHEN** screening blocks text or is unavailable
- **THEN** text is hidden and the star rating remains

### Requirement: Review privacy and backup
Reviews SHALL join nightly dumps and exports. Account deletion SHALL erase text and attribution and retain aggregate stars. Restore checks SHALL verify anonymization. Analytics review_submitted, review_deleted and $ai_generation SHALL use allowlisted identifiers, stars and enums without learner text and honor opt-out.

#### Scenario: Erasure
- **WHEN** a reviewer deletes their account
- **THEN** text and attribution disappear without changing the average

### Requirement: Review evaluation and responsive states
Offline pinned golden cases SHALL cover abusive, spam, multilingual, injection and safe false-block review inputs. The engine eval gate SHALL enforce configured thresholds. Desktop and mobile SHALL expose the same eligibility, loading, empty, edit, delete, offline recovery, keyboard, dark and reduced-motion states using design tokens.

#### Scenario: Retry safely
- **WHEN** submission fails offline
- **THEN** the form retains its draft and offers Retry
