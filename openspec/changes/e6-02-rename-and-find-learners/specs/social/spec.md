## ADDED Requirements

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

### Requirement: Search by username
`GET /v1/social/search?q=` SHALL return at most 20 matches (configured) for an exact Username or a prefix of at least 3 characters, each with only username, display name and avatar. Queries under 3 characters are refused with `search_too_short`. Searches SHALL be limited to 30 per minute per Learner (configured), refusing with `rate_limited`. The searcher is excluded. A Learner with `discoverable` off MUST appear only for an exact match and only when `request_policy` is `everyone`. Hidden and nonexistent Learners MUST be indistinguishable. Blocked pairs MUST be excluded once blocks exist (E6-04 adds the filter at one seam).

#### Scenario: Prefix
- **WHEN** a Learner searches "may"
- **THEN** discoverable matches are returned, exact first, at most 20

#### Scenario: Nondiscoverable
- **WHEN** a Learner with discovery off and requests from `everyone` is searched by prefix and then exactly
- **THEN** they are absent from the prefix results and present in the exact result

#### Scenario: Requests restricted
- **WHEN** a Learner with discovery off and requests from `nobody` is searched exactly
- **THEN** the result is empty, identical to a nonexistent name

### Requirement: Privacy settings
`GET`/`PATCH /v1/social/settings` SHALL read and change `discoverable`, `request_policy` (`everyone|friends_of_friends|nobody`) and `show_streak_to_friends` for the session Learner only. A failed save MUST keep the previous value. The UI SHALL offer them in Settings (Privacy & social) and the Friends Add flow shows the neutral not-found copy.

#### Scenario: Turn discovery off
- **WHEN** a Learner turns off "Findable by username"
- **THEN** prefix searches no longer return them

### Requirement: Rename and privacy data
Rename holds, `social_rate_limits` and privacy settings SHALL live in Postgres, be covered by the nightly dump and restore checks, and be removed on account erasure; the old-name hold stays 30 days as a hash of the previous owner.

#### Scenario: Restore
- **WHEN** a dump is restored
- **THEN** the rename hold, privacy settings and changed_at are intact
