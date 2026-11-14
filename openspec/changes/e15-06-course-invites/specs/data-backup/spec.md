## ADDED Requirements

### Requirement: Course invites are backed up and purged
`course_invites` and `course_invite_deliveries` SHALL be in the nightly encrypted Postgres dump. Invites that are accepted, declined, expired or revoked SHALL be purged 90 days later, and account deletion MUST erase invites the Learner sent or received.

#### Scenario: Final invites purge
- **WHEN** an invite resolved more than 90 days ago is swept
- **THEN** it and its delivery rows are gone and a pending invite remains
