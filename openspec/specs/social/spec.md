# social Specification

## Purpose
Let Learners claim a unique public Username, find each other, connect as friends and block one another, with server-enforced privacy settings. It also covers course-invite authorization between Learners and the cleanup of social data when an account is deleted.

## Requirements

### Requirement: Usernames
Every Learner SHALL have a unique Username, chosen at onboarding. A Username MUST be 3 to 20 characters of `a-z`, `0-9`, `_` and `.`, start with a letter, not end with a separator and not contain two separators in a row. Uniqueness MUST be checked case-insensitively on the NFKC-normalized lowercase form, and the chosen display case MUST be kept. Reserved names (configured list including `admin`, `support`, `duolify`, `moderator`) and names matching the profanity filter MUST be refused. Concurrent claims of the same name MUST result in exactly one success.

See `Username claim` for related detailed requirements.

#### Scenario: Case-insensitive clash
- **WHEN** "Maya" exists and another Learner picks "maya"
- **THEN** the API refuses with `username_taken`

#### Scenario: Reserved name
- **WHEN** a Learner picks "support"
- **THEN** the API refuses with `username_reserved`

#### Scenario: Race
- **WHEN** two Learners claim the same free Username at the same time
- **THEN** one succeeds and the other gets `username_taken`

### Requirement: Rename cooldown
A Learner SHALL be able to change their Username at most once every 30 days. A released Username MUST be held for 30 days and be claimable only by its previous owner in that time. Course invites and friend requests in flight MUST keep working after a rename.

See `Username rename` for related detailed requirements.

#### Scenario: Too soon
- **WHEN** a Learner renamed 10 days ago and tries again
- **THEN** the API refuses with the date the cooldown ends

#### Scenario: Old name held
- **WHEN** another Learner tries to claim a name released 5 days ago
- **THEN** the API refuses with `username_taken`

### Requirement: Search by username
`GET /v1/social/search?q=` SHALL return at most 20 matches (configured) for an exact Username or a prefix of at least 3 characters, each with only username, display name and avatar. Queries under 3 characters are refused with `search_too_short`. Searches SHALL be limited to 30 per minute per Learner (configured), refusing with `rate_limited`. The searcher is excluded. A Learner with `discoverable` off MUST appear only for an exact match and only when `request_policy` is `everyone`. Hidden and nonexistent Learners MUST be indistinguishable. Blocked pairs MUST be excluded once blocks exist (E6-04 adds the filter at one seam).

#### Scenario: Prefix search
- **WHEN** a Learner searches "may"
- **THEN** discoverable matches are returned with only the allowed fields

#### Scenario: Short query
- **WHEN** the query has 2 characters
- **THEN** the API refuses with a validation error

#### Scenario: Blocked
- **WHEN** Learner A blocked Learner B
- **THEN** B does not appear in A's results and A does not appear in B's

#### Scenario: Prefix
- **WHEN** a Learner searches "may"
- **THEN** discoverable matches are returned, exact first, at most 20

#### Scenario: Nondiscoverable
- **WHEN** a Learner with discovery off and requests from `everyone` is searched by prefix and then exactly
- **THEN** they are absent from the prefix results and present in the exact result

#### Scenario: Requests restricted
- **WHEN** a Learner with discovery off and requests from `nobody` is searched exactly
- **THEN** the result is empty, identical to a nonexistent name

### Requirement: Friend list requests
A Learner SHALL be able to send a friend request to a Username, and the recipient SHALL be able to accept or decline it; either Learner SHALL be able to remove a friend later. Friendship MUST need acceptance by the recipient. A sender MUST have at most 20 pending requests and send at most 50 per day, and a Friend list MUST hold at most 500 friends (limits are config). A declined request MUST stop the same sender from re-sending to that recipient for 7 days.

See `Friend requests by username or link` for related detailed requirements.

#### Scenario: Accept
- **WHEN** the recipient accepts
- **THEN** both Learners appear on each other's Friend list

#### Scenario: Re-send after decline
- **WHEN** the sender re-sends within 7 days of a decline
- **THEN** the API refuses without telling the recipient

#### Scenario: Remove
- **WHEN** a Learner removes a friend
- **THEN** the Friend list entry and the Friend streak between them end for both

### Requirement: Blocking
A Learner SHALL be able to block another Learner. Blocking MUST remove any friendship, cancel pending requests and Course invites in both directions, hide each from the other's search, and prevent new requests and invites. The blocked Learner MUST NOT be told they are blocked.

See `Block and unblock a learner` for related detailed requirements.

#### Scenario: Block a friend
- **WHEN** A blocks friend B
- **THEN** the friendship ends, B's pending invites to A are cancelled, and B's later requests to A silently fail

### Requirement: Privacy settings
`GET`/`PATCH /v1/social/settings` SHALL read and change `discoverable`, `request_policy` (`everyone|friends_of_friends|nobody`) and `show_streak_to_friends` for the session Learner only. A failed save MUST keep the previous value. The UI SHALL offer them in Settings (Privacy & social) and the Friends Add flow shows the neutral not-found copy.

#### Scenario: Requests off
- **WHEN** a Learner sets `friend_requests_from` to `nobody`
- **THEN** new requests to them are refused with a neutral error

#### Scenario: Profile data
- **WHEN** a friend opens a Learner's card
- **THEN** no Learner profile, notes, chat or email is returned

#### Scenario: Turn discovery off
- **WHEN** a Learner turns off "Findable by username"
- **THEN** prefix searches no longer return them

### Requirement: Social analytics events
The system SHALL emit `username_set`, `username_changed`, `username_rejected` (reason), `username_searched` (query_length, result_count), `friend_request_sent` (method `invite_link` or `username`), `friend_request_accepted`, `friend_request_declined`, `friend_removed`, `learner_blocked` and `privacy_setting_changed` (setting, value) with properties from the `analytics` spec. Events MUST NOT contain the searched text or other Learners' Usernames.

#### Scenario: Search event
- **WHEN** a Learner searches by prefix
- **THEN** `username_searched` has the length and count and not the query

### Requirement: Social data backup and retention
Usernames, rename history, friendships, requests, blocks and privacy settings MUST be stored in Postgres and covered by the nightly dump. When a Learner deletes their account, the Username MUST be released after the 30-day hold, friendships, requests and invites MUST be removed, and blocks the Learner placed and blocks placed against them MUST be removed (no hash is retained).

#### Scenario: Account deleted
- **WHEN** a Learner deletes their account
- **THEN** their friendships and requests disappear and the Username is claimable after 30 days

#### Scenario: Restore
- **WHEN** the database is restored
- **THEN** Usernames remain unique and the hold list is intact

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

### Requirement: Username rename
A signed-in Learner with a Username SHALL be able to change it, at most once per configured cooldown (30 days) counted from their last change. The old name MUST be held for the configured hold (30 days) and be claimable only by its previous owner, who MAY reclaim it inside the hold without waiting for the cooldown. A refusal for cooldown SHALL include the date the cooldown ends. In-flight references MUST stay Learner-ID based. Rename SHALL apply the same shape, reserved and profanity rules as a claim.

#### Scenario: Too soon
- **WHEN** a Learner renamed 10 days ago and renames again
- **THEN** the API refuses with `username_cooldown` and `next_change_at`

#### Scenario: Old name held
- **WHEN** another Learner claims a name released 5 days ago
- **THEN** the API refuses with `username_taken`, while the previous owner can reclaim it

#### Scenario: Race for a released name
- **WHEN** the previous owner and another Learner claim the released name at once
- **THEN** only the previous owner succeeds

### Requirement: Username attempt rate limit
Username claim and rename attempts SHALL be limited per Learner per hour (configured), refusing with `rate_limited` and `Retry-After`, so names cannot be probed.

#### Scenario: Probing
- **WHEN** a Learner exceeds the hourly attempt limit
- **THEN** further attempts are refused without evaluating the name

### Requirement: Rename and privacy data
Rename holds, `social_rate_limits` and privacy settings SHALL live in Postgres, be covered by the nightly dump and restore checks, and be removed on account erasure; the old-name hold stays 30 days as a hash of the previous owner.

#### Scenario: Restore
- **WHEN** a dump is restored
- **THEN** the rename hold, privacy settings and changed_at are intact

### Requirement: Friend requests by username or link
A Learner SHALL send a friend request by Username or by an opaque friend-link token. The recipient MUST accept for a friendship to exist. Send, accept, decline, cancel and remove MUST be idempotent. A mutual pending request MUST become one friendship. Limits (config): 20 pending outgoing, 50 sends per day, 500 friends, 7-day re-send cooldown after a decline.

#### Scenario: Accept
- **WHEN** the recipient accepts a pending request twice
- **THEN** one friendship exists and both calls succeed

#### Scenario: Concurrent mutual requests
- **WHEN** A and B send requests to each other at the same time
- **THEN** exactly one friendship exists and no pending request remains

#### Scenario: Re-send after decline
- **WHEN** the sender re-sends within 7 days of a decline
- **THEN** the API returns the neutral `request_unavailable` and nothing is created

### Requirement: Request policy and neutral errors
The API MUST enforce the recipient's `request_policy` (`everyone`, `friends_of_friends`, `nobody`) on the server. A missing learner, a refusing policy, no common friend, an active cooldown and a self request MUST be indistinguishable.

#### Scenario: Requests off
- **WHEN** a Learner whose policy is `nobody` receives a request
- **THEN** the sender sees the same error as for a username that does not exist

### Requirement: Friend link
A Learner SHALL create, rotate and revoke one friend link. Only a SHA-256 hash is stored; the token expires after 30 days (config). Opening a link MUST require a signed-in Learner and MUST NOT create a request until the Learner confirms. Expired, revoked or unknown links MUST return one neutral error.

#### Scenario: Revoked link
- **WHEN** the owner revokes the link and someone opens it
- **THEN** the opener sees the expired-invite state and no request is created

### Requirement: Remove a friend
Either friend SHALL remove the friendship. Removal MUST be one transaction that deletes the friendship and the pair's requests and is the single place the friend streak ends. The other Learner MUST NOT be notified.

#### Scenario: Remove
- **WHEN** a Learner removes a friend
- **THEN** both Friend lists drop the entry and a new request needs the usual checks again

### Requirement: Friend views
Friend and request lists MUST return only Username, display name, avatar and a privacy-permitted streak; never profile, notes, chat or email.

#### Scenario: Friend list
- **WHEN** a Learner opens the Friend list
- **THEN** each entry has only those fields

### Requirement: Friend data backup and erasure
`friend_requests`, `friendships` and `friend_links` (hash only) MUST be in nightly dumps. Account deletion MUST remove all rows referencing the Learner.

#### Scenario: Account deleted
- **WHEN** a Learner deletes their account
- **THEN** their friendships, requests and links are gone and former friends see them disappear

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

### Requirement: Social cleanup on deletion and restore
Deleting a Learner account SHALL remove their friendships, requests, friend links, nudges and shared streaks in the same operation, and the surviving friend MUST see no trace of the erased identity. The released Username MUST stay held for the configured hold. Replaying deletion tombstones after a restore MUST give the same result without breaking Username uniqueness or other active holds.

#### Scenario: Survivor view
- **WHEN** a connected Learner's account is deleted
- **THEN** the other Learner's Friend list, inbox and streak show nothing of them

#### Scenario: Hold survives restore
- **WHEN** a restored database replays a deletion tombstone
- **THEN** the Username is held and no other Learner can claim it before the hold ends
