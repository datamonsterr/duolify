## MODIFIED Requirements

### Requirement: Streak freeze
A Streak freeze SHALL preserve a Streak for one missed day. The system MUST apply an owned freeze automatically at rollover, without Learner action. Freezes MUST be earned through effort: Streak milestones set in config (for example every 7 days), and Friend streak milestones. Freezes MUST NOT be bought with Credits and MUST NOT be granted as Credits. The number held MUST be capped by config.

#### Scenario: Freeze consumed
- **WHEN** a day is missed and the Learner owns a freeze
- **THEN** one freeze is consumed and the Streak is unchanged
- **AND** `streak_freeze_used` is emitted

#### Scenario: Freeze earned
- **WHEN** a Streak reaches a freeze milestone and the Learner is below the cap
- **THEN** one freeze is added once for that milestone and `streak_freeze_earned` is emitted

#### Scenario: Cap reached
- **WHEN** a Learner at the freeze cap reaches another freeze milestone
- **THEN** the freeze is not added and no Credits are granted

#### Scenario: No purchase path
- **WHEN** a Learner looks for a way to buy a freeze with Credits
- **THEN** none exists in the API or the client
