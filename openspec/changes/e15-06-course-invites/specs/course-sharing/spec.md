## ADDED Requirements

### Requirement: Invite delivery and push adapter
The API SHALL deliver an invite to the recipient's in-app inbox whenever the invite is allowed, and SHALL send push only through an adapter that is disabled unless configured. Delivery MUST be deduplicated per invite and channel, MUST re-check blocks and the recipient's settings at send time, and MUST carry only identifiers and an enum, never Course material or a Username. A denied, disabled or failing push MUST NOT remove the in-app invite. Push credentials MUST come only from secret-store environment variables.

#### Scenario: Push denied
- **WHEN** the recipient turned course invite push off or the adapter is disabled
- **THEN** the invite is still in the recipient's inbox and no push is sent

#### Scenario: Neutral refusal
- **WHEN** the recipient's `course_invites_from` is `nobody` or the pair is blocked
- **THEN** the sender sees the invite as sent and the recipient's inbox and push stay empty

#### Scenario: Accept once
- **WHEN** the recipient accepts twice, or already owns a copy
- **THEN** exactly one pinned copy exists and the second accept returns the same copy
