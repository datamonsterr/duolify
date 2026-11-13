## ADDED Requirements

### Requirement: Rename and search event catalogue
The analytics catalogue SHALL include `username_changed` (integer `username_length`), `username_searched` (integer `query_length`, integer `result_count`) and `privacy_setting_changed` (`setting` in `discoverable|request_policy|show_streak_to_friends`, `value` in `on|off|everyone|friends_of_friends|nobody`). They SHALL use Learner UUID distinct IDs and common properties and respect analytics opt-out. Usernames, display names and search text MUST NOT be sent.

#### Scenario: Search event
- **WHEN** a Learner searches by prefix
- **THEN** `username_searched` has the query length and result count and no text
