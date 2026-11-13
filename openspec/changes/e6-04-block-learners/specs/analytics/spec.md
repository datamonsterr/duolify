## ADDED Requirements

### Requirement: Block event catalogue
The analytics catalogue SHALL include `learner_blocked` (`was_friend` boolean) and SHALL extend `privacy_setting_changed` with the setting `course_invites_from` (values `friends|everyone|nobody`). Events MUST use Learner UUID distinct IDs and common properties and respect analytics opt-out. Usernames, display names and the blocked Learner's identifier MUST NOT be sent. No unblock event exists.

#### Scenario: Block a friend
- **WHEN** a Learner blocks a friend
- **THEN** `learner_blocked` has `was_friend=true` and no other Learner's data
