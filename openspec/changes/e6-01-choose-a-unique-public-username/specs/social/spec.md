## ADDED Requirements

### Requirement: Username claim
The API SHALL let a signed-in Learner without a Username claim one. The name MUST be 3 to 20 characters of `a-z`, `0-9`, `_` and `.` after NFKC normalization, start with a letter, not end with a separator and not contain two separators in a row. Uniqueness SHALL be enforced by a Postgres unique index on the NFKC lowercase canonical form while the chosen display case is kept. Reserved and profanity lists SHALL come from configuration, and names confusable with them SHALL be refused. Concurrent claims of one name SHALL produce exactly one winner; losers receive the neutral `username_taken`.

#### Scenario: Case clash
- **WHEN** "Maya" exists and another Learner claims "mAYA"
- **THEN** the API refuses with `username_taken`

#### Scenario: Reserved
- **WHEN** a Learner claims "Support" or "supp0rt"
- **THEN** the API refuses with `username_reserved`

### Requirement: Minimal public identity
The public projection SHALL contain only username, display name, avatar and a streak status when the owner's `show_streak_to_friends` permits it. It MUST NOT contain email, profile, notes, chat or goals, and unknown or hidden Learners MUST get the same neutral not-found.

#### Scenario: Projection
- **WHEN** another Learner reads a public identity
- **THEN** only the allowed fields are returned

### Requirement: Username analytics
The API SHALL allowlist and catalogue `username_set` (username_length) and `username_rejected` (reason_code), with common properties and the opt-out gate. Events MUST NOT contain the username or display name.

#### Scenario: Rejected claim
- **WHEN** a claim is rejected
- **THEN** `username_rejected` carries a reason code and no text

### Requirement: Username backup and deletion hold
Usernames, holds and social settings SHALL be stored in Postgres, covered by the nightly dump and restored with uniqueness and holds intact. Account deletion SHALL remove the identity mapping, export SHALL include it, and the released name SHALL stay reserved for 30 days.

#### Scenario: Deleted account
- **WHEN** a Learner deletes their account
- **THEN** the name is refused for other Learners for 30 days
