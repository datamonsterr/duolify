## MODIFIED Requirements

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

### Requirement: Friend data backup and retention
Friendships, requests, blocks, Friend streak state and nudges MUST be stored in Postgres and covered by the nightly dump. Nudges MUST expire after `nudge_retention_days`. When a Learner deletes their account, friendships, requests, links and nudges MUST be removed and the Friend streak MUST end for the other Learner without revealing the deleted Learner's identity. After a restore, replayed deletion tombstones MUST do the same while Usernames stay unique and active holds stay intact.

#### Scenario: Account deleted
- **WHEN** a Learner deletes their account
- **THEN** the friendship, shared streak and nudges are removed and the friend's list and inbox contain nothing of the deleted Learner

#### Scenario: Restore replay
- **WHEN** a restored database replays the deletion tombstone of a connected Learner
- **THEN** the same rows are removed, the released Username is held and other holds are unchanged
