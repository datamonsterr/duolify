## ADDED Requirements

### Requirement: Enroll a pinned copy from a live link
A signed-in Learner SHALL create exactly one Course copy per source Course from a live Share link. The copy MUST be pinned to the latest passing published version, owned by the Learner, and idempotent on repeat. A private, revoked, unknown, deleted or block-hidden link MUST refuse new enrollment with the neutral `link_not_found`; existing copies MUST keep working. Per-account enrollments per day MUST be limited by config and refused with `rate_limited`.

#### Scenario: Enroll twice
- **WHEN** a Learner enrolls through the same link twice
- **THEN** one copy exists and both calls return its id, the second with `created=false`

#### Scenario: Revoked link
- **WHEN** the owner revokes the link and a new Learner tries to enroll
- **THEN** the API returns `link_not_found`, no copy is created, and existing copies still open

### Requirement: Reuse published lessons for free
Lessons already generated in the pinned version MUST be served from the copy at zero Credits (no quote, reservation or charge) and MUST NOT expose answer keys in manifests. Lesson gate order MUST still require the predecessor pass. Lessons missing from the snapshot MUST be reported as needing generation, left for E15-04.

#### Scenario: Unlock without spending
- **WHEN** a Learner passes Lesson 1 of a copy whose version contains Lesson 2
- **THEN** Lesson 2 becomes `current` and the Credit balance is unchanged

#### Scenario: Learner-specific state
- **WHEN** two Learners enroll from one link and one passes Lesson 1
- **THEN** the other still has Lesson 2 locked, and the creator sees only the enrollment count
