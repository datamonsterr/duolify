## ADDED Requirements

### Requirement: Usernames
Every Learner SHALL have a unique Username, chosen at onboarding. A Username MUST be 3 to 20 characters of `a-z`, `0-9`, `_` and `.`, start with a letter, not end with a separator and not contain two separators in a row. Uniqueness MUST be checked case-insensitively on the NFKC-normalized lowercase form, and the chosen display case MUST be kept. Reserved names (configured list including `admin`, `support`, `duolify`, `moderator`) and names matching the profanity filter MUST be refused. Concurrent claims of the same name MUST result in exactly one success.

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

#### Scenario: Too soon
- **WHEN** a Learner renamed 10 days ago and tries again
- **THEN** the API refuses with the date the cooldown ends

#### Scenario: Old name held
- **WHEN** another Learner tries to claim a name released 5 days ago
- **THEN** the API refuses with `username_taken`

### Requirement: Search by username
A signed-in Learner SHALL be able to find another Learner by exact Username or by prefix of at least 3 characters. Results MUST be limited to 20, rate limited to 30 searches per minute, and MUST show only Username, display name and avatar. Learners with `discoverable` off MUST be found only by exact Username when they have allowed requests from everyone. A blocked pair MUST NOT see each other in results.

#### Scenario: Prefix search
- **WHEN** a Learner searches "may"
- **THEN** discoverable matches are returned with only the allowed fields

#### Scenario: Short query
- **WHEN** the query has 2 characters
- **THEN** the API refuses with a validation error

#### Scenario: Blocked
- **WHEN** Learner A blocked Learner B
- **THEN** B does not appear in A's results and A does not appear in B's

### Requirement: Friend list requests
A Learner SHALL be able to send a friend request to a Username, and the recipient SHALL be able to accept or decline it; either Learner SHALL be able to remove a friend later. Friendship MUST need acceptance by the recipient. A sender MUST have at most 20 pending requests and send at most 50 per day, and a Friend list MUST hold at most 500 friends (limits are config). A declined request MUST stop the same sender from re-sending to that recipient for 7 days.

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

#### Scenario: Block a friend
- **WHEN** A blocks friend B
- **THEN** the friendship ends, B's pending invites to A are cancelled, and B's later requests to A silently fail

### Requirement: Privacy settings
A Learner SHALL control `discoverable` (default on), `friend_requests_from` (`everyone` or `nobody`), `course_invites_from` (`friends`, `everyone` or `nobody`, default `friends`) and `show_streak_to_friends`. Settings MUST apply on the next request. The API MUST expose to other Learners only Username, display name, avatar and, to friends with the setting on, Streak length and today's completion status.

#### Scenario: Requests off
- **WHEN** a Learner sets `friend_requests_from` to `nobody`
- **THEN** new requests to them are refused with a neutral error

#### Scenario: Profile data
- **WHEN** a friend opens a Learner's card
- **THEN** no Learner profile, notes, chat or email is returned

### Requirement: Social analytics events
The system SHALL emit `username_set`, `username_changed`, `username_rejected` (reason), `username_searched` (query_length, result_count), `friend_request_sent` (method `invite_link` or `username`), `friend_request_accepted`, `friend_request_declined`, `friend_removed`, `learner_blocked` and `privacy_setting_changed` (setting, value) with properties from the `analytics` spec. Events MUST NOT contain the searched text or other Learners' Usernames.

#### Scenario: Search event
- **WHEN** a Learner searches by prefix
- **THEN** `username_searched` has the length and count and not the query

### Requirement: Social data backup and retention
Usernames, rename history, friendships, requests, blocks and privacy settings MUST be stored in Postgres and covered by the nightly dump. When a Learner deletes their account, the Username MUST be released after the 30-day hold, friendships, requests and invites MUST be removed, and blocks the Learner placed MUST be removed; blocks placed against them MAY be kept as an opaque hash so the blocker is not re-exposed.

#### Scenario: Account deleted
- **WHEN** a Learner deletes their account
- **THEN** their friendships and requests disappear and the Username is claimable after 30 days

#### Scenario: Restore
- **WHEN** the database is restored
- **THEN** Usernames remain unique and the hold list is intact
