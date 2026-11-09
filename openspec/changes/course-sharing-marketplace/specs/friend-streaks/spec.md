## MODIFIED Requirements

### Requirement: Friend connections
A Learner SHALL be able to send, accept and decline friend requests by invite link or by Username as defined in the `social` capability. A friendship MUST need acceptance by both Learners. A Learner MUST be able to remove a friend or block a Learner, with the effects defined in `social`.

#### Scenario: Request accepted
- **WHEN** a Learner accepts a friend request
- **THEN** both Learners see each other as friends

#### Scenario: Blocked Learner
- **WHEN** a Learner blocks another
- **THEN** pending requests and the Friend streak between them end
