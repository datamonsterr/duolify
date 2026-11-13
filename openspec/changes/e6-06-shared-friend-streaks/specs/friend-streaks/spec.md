## ADDED Requirements

### Requirement: Shared day projection
The system SHALL project both friends' completion and protection days into one Friend streak per friendship. A shared day pairs each friend's own local date whose real-time intervals overlap, each friend sees their own local date, and the later of the two day-ends decides rollover. The streak MUST grow by one at most once per shared day, the pair-day key being unique in Postgres, and MUST NOT count days before the friendship started.

#### Scenario: Both complete in different time zones
- **WHEN** friends in Auckland and Los Angeles each complete a Lesson on their own local date and those dates overlap in real time
- **THEN** the Friend streak grows by one exactly once, however often the projection runs

#### Scenario: Waiting for the later day-end
- **WHEN** one friend's day has ended without a completion and the other friend's day is still open
- **THEN** the Friend streak is not broken yet and each friend's local-day status is shown

### Requirement: Protect missed sides with personal freezes
A missed shared day MUST keep the Friend streak only when every missing side is protected by that learner's own E5 protection for their local date; otherwise the Friend streak breaks. A protected day does not increase the length. No second freeze inventory exists: one personal protection serves every Friend streak of that learner and no freeze is spent from the other friend.

#### Scenario: One side missed with a freeze
- **WHEN** one friend misses a day and holds a freeze
- **THEN** that friend's freeze protects the day, the Friend streak length is unchanged and the other friend's freeze is untouched

#### Scenario: Both missed
- **WHEN** both friends miss a day and only one is protected
- **THEN** the Friend streak breaks

#### Scenario: Many friends
- **WHEN** a Learner misses a day while having several Friend streaks
- **THEN** exactly one freeze is consumed

### Requirement: Friend streak milestone freezes
Reaching a multiple of the configured milestone SHALL grant each Learner one Streak freeze below the E5 cap, once per pair run and milestone and once per Learner, run start and milestone, never as Credits. At or above the cap the milestone is recorded without a grant.

#### Scenario: Milestone below cap
- **WHEN** a Friend streak reaches the milestone and both Learners hold fewer freezes than the cap
- **THEN** each Learner's freeze count rises by one and `streak_freeze_earned` has reason `friend_streak`

#### Scenario: Milestone replay
- **WHEN** the same milestone is evaluated again
- **THEN** no further freeze or event is created

### Requirement: Friend streak ends with the friendship
Removing a friend, blocking a Learner and deleting an account MUST end the Friend streak in the same transaction that ends the friendship, deleting pair state without leaving personal data of the erased Learner.

#### Scenario: Remove friend
- **WHEN** a Learner removes a friend with a live Friend streak
- **THEN** the pair state is gone and `friend_streak_broken` is emitted with reason `friendship_ended`

### Requirement: Friend streak view privacy
The Friends list SHALL include for each friend only the Friend streak length, state and both learners' local date with a done flag, and only when the friend's `show_streak_to_friends` is on and the pair is not blocked.

#### Scenario: Setting off
- **WHEN** a friend turns `show_streak_to_friends` off
- **THEN** the Friends list returns no streak data for that friend

### Requirement: Friend streak data backup
Friend streak state, pair-day keys and milestone keys MUST be stored in Postgres and covered by the nightly dump and restore checks. They are erased with the friendship or either account.

#### Scenario: Restore
- **WHEN** a nightly dump is restored
- **THEN** the pair state, its days and milestone keys equal the originals
