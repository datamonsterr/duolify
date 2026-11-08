# friend-streaks Specification

## Purpose
Let two Learners keep a shared Friend streak that grows when both complete a Lesson on the same day. It rewards effort together.

## Requirements

### Requirement: Friend connections
A Learner SHALL be able to send, accept and decline friend requests by invite link or username. A friendship MUST need acceptance by both Learners. A Learner MUST be able to remove a friend or block a Learner.

#### Scenario: Request accepted
- **WHEN** a Learner accepts a friend request
- **THEN** both Learners see each other as friends

#### Scenario: Blocked Learner
- **WHEN** a Learner blocks another
- **THEN** pending requests and the Friend streak between them end

### Requirement: Friend streak growth
A Friend streak SHALL grow by one for a day when both friends complete at least one Lesson that local day. The day MUST be evaluated in the time zone of each Learner, and the later of the two day-ends MUST decide rollover.

#### Scenario: Both complete
- **WHEN** both friends complete a Lesson on the same day
- **THEN** the Friend streak increases by one and `friend_streak_extended` is emitted

#### Scenario: One misses
- **WHEN** a day ends and only one friend completed a Lesson
- **THEN** the Friend streak breaks unless a Streak freeze is applied

### Requirement: Streak freeze interaction
A Friend streak SHALL be preserved by a Streak freeze owned by the friend who missed, at that friend's choice per config. A freeze used for a Friend streak MUST count as that friend's freeze consumption.

#### Scenario: Freeze saves Friend streak
- **WHEN** one friend misses a day and owns a freeze
- **THEN** the freeze is consumed and the Friend streak is unchanged

### Requirement: Nudges
A Learner MAY nudge a friend who has not yet completed a Lesson today. The API MUST limit nudges to one per friend per day.

#### Scenario: Second nudge same day
- **WHEN** a Learner nudges the same friend twice in a day
- **THEN** the second request is rejected

### Requirement: Friend streak rewards
Reaching Friend streak milestones in config SHALL grant both Learners reward Credits and, when set, a Streak freeze, via the ledger kind `reward`.

#### Scenario: Milestone reached
- **WHEN** a Friend streak reaches a milestone
- **THEN** each Learner receives the reward once and `reward_credits_granted` is emitted

### Requirement: Friend streak analytics events
The system SHALL emit `friend_request_sent`, `friend_request_accepted`, `friend_streak_started`, `friend_streak_extended`, `friend_streak_broken` and `friend_nudge_sent` with properties from the `analytics` spec.

#### Scenario: Request sent
- **WHEN** a friend request is sent
- **THEN** `friend_request_sent` is emitted with `method` (`invite_link` or `username`)

### Requirement: Friend data backup and retention
Friendships, requests, blocks, Friend streak state and nudges MUST be stored in Postgres and covered by the nightly dump. When a Learner deletes their account, friendships MUST be removed and the Friend streak MUST end for the other Learner.

#### Scenario: Account deleted
- **WHEN** a Learner deletes their account
- **THEN** the friendship rows are removed and the friend sees the streak end without personal data of the deleted Learner

### Requirement: Privacy
The system MUST expose to a friend only display name, avatar, Streak length and today's completion status. It MUST NOT expose Learner profile, notes or chat to friends.

#### Scenario: Friend views profile
- **WHEN** a friend opens a Learner's card
- **THEN** only the allowed fields are returned
