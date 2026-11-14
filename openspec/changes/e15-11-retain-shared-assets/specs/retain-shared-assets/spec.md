## ADDED Requirements

### Requirement: Unpublish preserves copies
The owner SHALL be able to unpublish a course. Unpublish MUST set visibility `private`, revoke the share link, delist the Marketplace listing, refuse new enrollments and invites, and leave every existing copy usable at its pinned version. The action MUST be idempotent and emit `course_visibility_changed`.

#### Scenario: Unpublish with a copy
- **WHEN** the owner unpublishes a shared course that a learner copied
- **THEN** the link answers not found, new enrollment is refused and the copy still opens

### Requirement: Owner deletion anonymizes attribution
When the owner deletes the course or their account, copies MUST keep working and MUST show the creator as "deleted user"; no owner personal data remains on a copy.

#### Scenario: Owner account deleted
- **WHEN** the owner deletes their account
- **THEN** the learner's copy still plays and shows "Created by deleted user"

### Requirement: Reference-counted asset retention
Published lesson objects (HTML, test and narration assets) SHALL be kept while any live copy pins them or references them from a lesson manifest. When the last reference ends they MUST be released and purged after a configured period (default 30 days) by a retention job. A concurrent enrollment or update MUST NOT lose objects to a purge.

#### Scenario: Last copy ends
- **WHEN** the only copy of a version is deleted
- **THEN** the prefix is released, purged only after 30 days, and a new copy within that time cancels the purge

### Requirement: Moderation blocks survive restore
A severe takedown MUST be recorded in the backup ledger before it commits, and a restore MUST re-apply it; an overturned action MUST write a lift marker.

#### Scenario: Restore of an older dump
- **WHEN** a dump taken before a severe takedown is restored
- **THEN** the drill re-blocks the affected copies

### Requirement: Restore resolves pins and objects
Restore drills MUST verify that every copy pin resolves to a retention row and that every referenced published object exists.

#### Scenario: Missing object
- **WHEN** a pinned object is absent from storage
- **THEN** the drill reports a failed objects check

### Requirement: Release readiness without release
A moderator-only report SHALL list publish, enrollment, invite, review and report caps, retention and erasure policy, noindex and CDN policy and the owner/legal approvals as satisfied or not. The report MUST NOT change `MARKETPLACE_ENABLED`.

#### Scenario: Approvals missing
- **WHEN** legal approval is not configured
- **THEN** the report shows it unsatisfied and the Marketplace stays off
