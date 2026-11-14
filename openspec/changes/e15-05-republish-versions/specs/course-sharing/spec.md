## ADDED Requirements

### Requirement: Stable lesson keys and explicit updates
Every published Lesson SHALL carry a stable key that survives retitles and reordering. A Learner's Course copy SHALL move to a newer version only when the Learner updates it with the changelog shown; the update MUST map Lessons by key, keep completed Lessons, tests, progress, notes and Learner-generated Lessons, add new Lessons, and keep Lessons removed upstream in the Learner's history as archived. A copy forked by Course steering MUST NOT be offered or receive updates. A pin or update with a stale lock version MUST be rejected without detail. Objects of a published version MUST be retained while any copy pins it or references its Lessons.

#### Scenario: Update with retitled and removed lessons
- **WHEN** the owner retitles Lesson 2, removes Lesson 3 and republishes, and a Learner who completed Lessons 1 and 2 updates
- **THEN** Lessons 1 and 2 stay completed with the new title, Lesson 3 appears as archived history and the pin is the new version

#### Scenario: Stale update
- **WHEN** two updates race with the same lock version
- **THEN** one succeeds and the other is rejected neutrally

### Requirement: Republish cap and screening
Republishing SHALL be limited to three versions per Course per rolling day (configuration) and every version SHALL be screened anew; only a passing version becomes live.

#### Scenario: Fourth republish
- **WHEN** an owner republishes a fourth time within a day
- **THEN** the request is refused with the limit error and no version is created
