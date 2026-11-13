## ADDED Requirements

### Requirement: Block and unblock a learner
A signed-in Learner SHALL block a Learner by Username from a friend row, a profile card or the Blocked list, and unblock from the Blocked list. Blocking MUST be one transaction that records the block, removes the friendship and its friend streak, deletes pending and decided requests in both directions and runs the registered on-block callbacks; a failing callback MUST roll everything back. Block and unblock MUST be idempotent. A Learner MAY block at most 500 Learners (config `block_max`). A target the actor cannot see, an unknown Username and self MUST return the neutral `block_unavailable`.

#### Scenario: Block a friend
- **WHEN** A blocks friend B who has a pending invite to A
- **THEN** no friendship and no request between them remains and B is not notified

#### Scenario: Block twice
- **WHEN** A blocks B again
- **THEN** the call succeeds and nothing changes

#### Scenario: Block race
- **WHEN** A blocks B while B sends A a request or A accepts B's request
- **THEN** exactly one block exists and no friendship or pending request remains

### Requirement: Blocked pair is hidden and refused
A blocked pair MUST NOT see each other in search results, profile lookups or friend-link previews, and a request or link accept in either direction MUST fail with the same neutral error as an unknown Username without creating or notifying anything. The blocked Learner MUST NOT be told they are blocked beyond the friendship ending. Unblocking MUST NOT restore any friendship, request or invite.

#### Scenario: Send to a blocker
- **WHEN** B sends a request to the Learner who blocked B
- **THEN** B receives the same `request_unavailable` as for a username that does not exist and no row is created

#### Scenario: Search both ways
- **WHEN** A and B are a blocked pair
- **THEN** neither appears in the other's search or profile

### Requirement: Shared interaction authorization
The `social` package SHALL export `BlockedEitherWay(ctx, q, a, b)`, `CanInteract(ctx, q, a, b)`, `CanInvite(ctx, q, inviter, invitee)`, `NotBlockedSQL(viewer, other)` and `Service.OnBlock(fn)`. Every Course invite and share code path MUST call `CanInvite` (or `CanInteract`) in the same transaction that writes the invite, and MUST register an on-block callback that cancels invites between the pair in both directions.

#### Scenario: Invite after block
- **WHEN** an invite is authorized between a blocked pair
- **THEN** `CanInteract` is false in both directions and `CanInvite` is false

#### Scenario: Invite setting
- **WHEN** the invitee's `course_invites_from` is `friends` and the pair are not friends
- **THEN** `CanInvite` is false; with `everyone` and no block it is true; with `nobody` it is always false

### Requirement: Privacy projection is server-enforced
Request policy, `course_invites_from` and `show_streak_to_friends` MUST apply on the next request wherever identity or streak status is projected. A profile card MUST include a streak only for a friend viewer when the owner's `show_streak_to_friends` is on.

#### Scenario: Streak hidden
- **WHEN** a friend's `show_streak_to_friends` is off
- **THEN** their profile card and the Friend list carry no streak

### Requirement: Block data backup and erasure
`learner_blocks` MUST be in nightly dumps and the restore check. Account deletion MUST remove blocks the Learner placed and blocks placed against them (no hash is retained). Data export MUST list the Learner's own blocks by Username and date only.

#### Scenario: Account deleted
- **WHEN** a Learner deletes their account
- **THEN** no `learner_blocks` row references them
