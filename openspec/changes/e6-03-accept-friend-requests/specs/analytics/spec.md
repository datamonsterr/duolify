## ADDED Requirements

### Requirement: Friend event catalogue
The analytics catalogue SHALL include `friend_request_sent` (`method` in `username|invite_link`), `friend_request_accepted`, `friend_request_declined` and `friend_removed` (no properties besides common ones). They SHALL use Learner UUID distinct IDs and common properties and respect analytics opt-out. Usernames, display names, tokens and request IDs MUST NOT be sent.

#### Scenario: Send by link
- **WHEN** a Learner sends a request through a friend link
- **THEN** `friend_request_sent` has `method=invite_link` and no other learner's data
