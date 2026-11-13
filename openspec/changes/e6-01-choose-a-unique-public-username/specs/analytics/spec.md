## ADDED Requirements

### Requirement: Username event catalogue
The analytics catalogue SHALL include `username_set` (integer `username_length`) and `username_rejected` (`reason_code` restricted to `username_invalid|username_taken|username_reserved|username_not_allowed|username_already_set`). Both SHALL use Learner UUID distinct IDs and common properties and respect analytics opt-out. Usernames, display names and other Learners' identifiers MUST NOT be sent.

#### Scenario: Rejected claim event
- **WHEN** a claim for a reserved name is refused
- **THEN** `username_rejected` is emitted with `reason_code=username_reserved` and no name text
