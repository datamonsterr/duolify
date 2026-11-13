## ADDED Requirements

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
