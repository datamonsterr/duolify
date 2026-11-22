# friend-streaks Specification

## Purpose
Let two Learners keep a shared Friend streak that grows when both complete a Lesson on the same day. It rewards effort together.

## Requirements

### Requirement: Friend connections
A Learner SHALL be able to send, accept and decline friend requests by invite link or by Username as defined in the `social` capability. A friendship MUST need acceptance by both Learners. A Learner MUST be able to remove a friend or block a Learner, with the effects defined in `social`.

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
A Friend streak SHALL be preserved on a missed day by the Streak freeze of the friend who missed. Each Learner's own freeze protects their own side only: a freeze MUST NOT be spent from, or counted against, the other friend. The freeze MUST be applied automatically at rollover, as for a Streak. A freeze used for a Friend streak MUST count as that friend's freeze consumption and MUST also preserve that friend's own Streak. If both friends miss the same day, each side MUST be evaluated with its own freeze, and the Friend streak breaks unless both sides are protected.

#### Scenario: Freeze saves Friend streak
- **WHEN** one friend misses a day and owns a freeze
- **THEN** that friend's freeze is consumed and the Friend streak is unchanged

#### Scenario: Friend without freeze misses
- **WHEN** one friend misses a day and has no freeze, while the other friend owns one
- **THEN** the Friend streak breaks and the other friend's freeze is not consumed

#### Scenario: Both miss
- **WHEN** both friends miss the same day and only one owns a freeze
- **THEN** the Friend streak breaks

### Requirement: Nudges
A Learner MAY nudge a friend who has not yet completed a Lesson today. The API MUST limit nudges to one per friend per sender local day, enforced by a Postgres unique key, and MUST treat a retry with the same operation key as the original request. Delivery MUST be in-app only. A block, a removed friendship or the recipient's `show_streak_to_friends` setting set to off MUST suppress delivery, and the recipient MUST NOT be revealed to the sender as the reason. Nudges MUST be kept for at most the configured retention.

#### Scenario: Second nudge same day
- **WHEN** a Learner nudges the same friend twice in a day with different operation keys
- **THEN** the second request is rejected with `nudge_limited` and delivers nothing

#### Scenario: Retry
- **WHEN** a Learner repeats a nudge with the same operation key
- **THEN** the original result is returned and the recipient has one inbox entry

#### Scenario: Suppressed
- **WHEN** the pair is blocked, no longer friends, or the recipient hides their streak from friends
- **THEN** no nudge appears in the recipient's inbox and the sender learns nothing about why

#### Scenario: Friend finished
- **WHEN** the friend already completed a Lesson today
- **THEN** the nudge is refused with the neutral `nudge_unavailable`

### Requirement: Friend streak analytics events
The system SHALL emit `friend_request_sent`, `friend_request_accepted`, `friend_streak_started`, `friend_streak_extended`, `friend_streak_broken` and `friend_nudge_sent` with properties from the `analytics` spec.

#### Scenario: Request sent
- **WHEN** a friend request is sent
- **THEN** `friend_request_sent` is emitted with `method` (`invite_link` or `username`)

### Requirement: Friend data backup and retention
Friendships, requests, blocks, Friend streak state and nudges MUST be stored in Postgres and covered by the nightly dump. Nudges MUST expire after `nudge_retention_days`. When a Learner deletes their account, friendships, requests, links and nudges MUST be removed and the Friend streak MUST end for the other Learner without revealing the deleted Learner's identity. After a restore, replayed deletion tombstones MUST do the same while Usernames stay unique and active holds stay intact.

#### Scenario: Account deleted
- **WHEN** a Learner deletes their account
- **THEN** the friendship, shared streak and nudges are removed and the friend's list and inbox contain nothing of the deleted Learner

#### Scenario: Restore replay
- **WHEN** a restored database replays the deletion tombstone of a connected Learner
- **THEN** the same rows are removed, the released Username is held and other holds are unchanged

### Requirement: Privacy
The system MUST expose to a friend only display name, avatar, Streak length and today's completion status. It MUST NOT expose Learner profile, notes or chat to friends.

#### Scenario: Friend views profile
- **WHEN** a friend opens a Learner's card
- **THEN** only the allowed fields are returned

### Requirement: Friend streak milestone freezes
Reaching a multiple of the configured milestone SHALL grant each Learner one Streak freeze below the E5 cap, once per pair run and milestone and once per Learner, run start and milestone, never as Credits. At or above the cap the milestone is recorded without a grant.

#### Scenario: Milestone reached
- **WHEN** a Friend streak reaches a milestone
- **THEN** each Learner below the cap receives one freeze once and `streak_freeze_earned` is emitted with reason `friend_streak`

#### Scenario: Milestone below cap
- **WHEN** a Friend streak reaches the milestone and both Learners hold fewer freezes than the cap
- **THEN** each Learner's freeze count rises by one and `streak_freeze_earned` has reason `friend_streak`

#### Scenario: Milestone replay
- **WHEN** the same milestone is evaluated again
- **THEN** no further freeze or event is created

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

### Requirement: Friend streak sweep cadence
Friend streak settlement SHALL continue to run after each learner's rollover and XP award. The periodic sweep exists only to repair a missed settlement: it MUST run only in the worker, settle a live pair only when it has gone 15 minutes without a check, and be idempotent across concurrent workers.

#### Scenario: Missed settlement
- **WHEN** a pair's day-end passed but no settlement ran and the pair was last checked more than 15 minutes ago
- **THEN** the sweep settles it once, even with two workers sweeping
