## MODIFIED Requirements

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
